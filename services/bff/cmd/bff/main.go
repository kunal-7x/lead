package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lead/services/bff/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := server.Config{
		Addr:          envOr("BFF_ADDR", ":8080"),
		TenantAuthURL: envOr("TENANT_AUTH_URL", "http://localhost:8101"),
		LeadImportURL: envOr("LEAD_IMPORT_URL", "http://localhost:8106"),
		CampaignURL:   envOr("CAMPAIGN_URL", "http://localhost:8112"),
		WhatsAppURL:   envOr("WHATSAPP_URL", "http://localhost:8119"),
		SchedulerURL:  envOr("SCHEDULER_URL", "http://localhost:8107"),
		AnalyticsURL:  envOr("ANALYTICS_URL", "http://localhost:8116"),
		RedisAddr:     envOr("REDIS_ADDR", "localhost:6379"),
		JWTSecret:     envOr("JWT_SECRET", "dev-secret"),
		AllowedOrigins: allowedOrigins(
			envOr("ALLOWED_ORIGIN", "http://localhost:3000"),
		),
	}

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create server", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      srv,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("bff listening", "addr", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("bff stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// allowedOrigins splits a comma-separated ALLOWED_ORIGIN env value into a slice,
// trimming whitespace from each entry.
func allowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
