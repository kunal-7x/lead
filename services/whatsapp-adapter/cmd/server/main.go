package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	libsvault "github.com/lead/libs/go/vault"
	"github.com/lead/services/whatsapp-adapter/internal/claimcontrol"
	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/handler"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	st, err := newStore()
	if err != nil {
		logger.Error("store init", "err", err)
		os.Exit(1)
	}
	if closer, ok := st.(interface{ Close() }); ok {
		defer closer.Close()
	}
	metaClient := meta.NewCloudClient(envOr("META_GRAPH_URL", "https://graph.facebook.com/v20.0"), http.DefaultClient)
	consentChecker := consent.StaticChecker{Allowed: true}
	svc := service.New(st, consentChecker, metaClient)
	svc.SetClaimControl(claimcontrol.NewHTTP(envOr("CLAIM_CONTROL_URL", envOr("KNOWLEDGE_SERVICE_URL", "http://knowledge:8110"))))

	cred := loadWACredential(logger)
	if err := validateWACredential(cred); err != nil {
		logger.Error("whatsapp credential invalid", "err", err)
		os.Exit(1)
	}
	_, _ = svc.UpsertCredential(context.Background(), cred)
	_ = svc.SeedPrebuiltTemplates(context.Background(), "default")
	if demoMode() {
		_ = svc.SeedDemoInbox(context.Background(), "tenant-demo")
	}

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler.New(svc).Mount(r)

	addr := envOr("WHATSAPP_ADAPTER_ADDR", ":8119")
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("whatsapp-adapter listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("whatsapp-adapter stopped")
}

// loadWACredential reads META_WA_* values from Vault when VAULT_ADDR is set,
// falling back to env vars so dev without Vault keeps working.
func loadWACredential(logger *slog.Logger) model.VaultCredential {
	cred := model.VaultCredential{
		TenantID:          "default",
		PhoneNumberID:     os.Getenv("META_WA_PHONE_NUMBER_ID"),
		BusinessAccountID: os.Getenv("META_WA_BUSINESS_ACCOUNT_ID"),
		AccessToken:       os.Getenv("META_WA_TOKEN"),
		VerifyToken:       os.Getenv("META_WA_VERIFY_TOKEN"),
		AppSecret:         os.Getenv("META_WA_APP_SECRET"),
	}

	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	if addr == "" || token == "" {
		return cred
	}

	vc, err := libsvault.New(addr, token)
	if err != nil {
		logger.Warn("vault client init failed — using env vars", "err", err)
		return cred
	}

	data, err := vc.ReadKV("capsy/whatsapp/default")
	if err != nil {
		logger.Warn("vault read whatsapp creds failed — using env vars", "err", err)
		return cred
	}

	if v := data["META_WA_PHONE_NUMBER_ID"]; v != "" {
		cred.PhoneNumberID = v
	}
	if v := data["META_WA_BUSINESS_ACCOUNT_ID"]; v != "" {
		cred.BusinessAccountID = v
	}
	if v := data["META_WA_TOKEN"]; v != "" {
		cred.AccessToken = v
	}
	if v := data["META_WA_VERIFY_TOKEN"]; v != "" {
		cred.VerifyToken = v
	}
	if v := data["META_WA_APP_SECRET"]; v != "" {
		cred.AppSecret = v
	}

	logger.Info("loaded secret from vault", "path", "capsy/whatsapp/default")
	return cred
}

func newStore() (store.Store, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return store.NewPostgres(dsn)
	}
	if !demoMode() {
		return nil, fmt.Errorf("DATABASE_URL is required outside DEMO_MODE")
	}
	return store.NewFake(), nil
}

func validateWACredential(cred model.VaultCredential) error {
	if demoMode() {
		return nil
	}
	if cred.PhoneNumberID == "" || cred.BusinessAccountID == "" || cred.AccessToken == "" || cred.VerifyToken == "" || cred.AppSecret == "" {
		return fmt.Errorf("META_WA_PHONE_NUMBER_ID, META_WA_BUSINESS_ACCOUNT_ID, META_WA_TOKEN, META_WA_VERIFY_TOKEN, and META_WA_APP_SECRET are required outside DEMO_MODE")
	}
	return nil
}

func demoMode() bool {
	return os.Getenv("DEMO_MODE") == "1"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
