package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/handler"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	st := store.NewFake()
	metaClient := meta.NewCloudClient(envOr("META_GRAPH_URL", "https://graph.facebook.com/v20.0"), http.DefaultClient)
	consentChecker := consent.StaticChecker{Allowed: true}
	svc := service.New(st, consentChecker, metaClient)

	_, _ = svc.UpsertCredential(context.Background(), model.VaultCredential{
		TenantID:          "default",
		PhoneNumberID:     os.Getenv("META_WA_PHONE_NUMBER_ID"),
		BusinessAccountID: os.Getenv("META_WA_BUSINESS_ACCOUNT_ID"),
		AccessToken:       os.Getenv("META_WA_TOKEN"),
		VerifyToken:       os.Getenv("META_WA_VERIFY_TOKEN"),
		AppSecret:         os.Getenv("META_WA_APP_SECRET"),
	})
	_ = svc.SeedPrebuiltTemplates(context.Background(), "default")
	_ = svc.SeedDemoInbox(context.Background(), "tenant-demo")

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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
