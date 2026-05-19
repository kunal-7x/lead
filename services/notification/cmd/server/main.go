package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	libsvault "github.com/lead/libs/go/vault"
	"github.com/lead/services/notification/internal/adapters"
	"github.com/lead/services/notification/internal/handler"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
	"github.com/lead/services/notification/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	loadSecretsFromVault(log)

	st, err := newStore()
	if err != nil {
		log.Error("store init", "error", err)
		os.Exit(1)
	}
	if closer, ok := st.(interface{ Close() }); ok {
		defer closer.Close()
	}
	svc := service.New(st, map[model.Channel]adapters.Adapter{
		model.ChannelDashboard: adapters.NewDashboard(),
		model.ChannelWhatsApp:  adapters.NewWhatsApp(),
		model.ChannelEmail:     adapters.NewEmail(),
		model.ChannelSlack:     adapters.NewWebhook(),
		model.ChannelDiscord:   adapters.NewWebhook(),
		model.ChannelCRM:       adapters.NewWebhook(),
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go startConsumer(ctx, log, svc)

	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)

	addr := envOr("NOTIFICATION_ADDR", ":8113")
	log.Info("notification listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

// loadSecretsFromVault fetches notification provider credentials from Vault
// and overrides the corresponding env vars. Adapters read from env vars so
// they remain unchanged; this is the only injection point.
func loadSecretsFromVault(log *slog.Logger) {
	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	if addr == "" || token == "" {
		return
	}

	vc, err := libsvault.New(addr, token)
	if err != nil {
		log.Warn("vault client init failed — using env vars", "err", err)
		return
	}

	data, err := vc.ReadKV("capsy/providers/notification")
	if err != nil {
		log.Warn("vault read notification secrets failed — using env vars", "err", err)
		return
	}

	keys := []string{
		"POSTMARK_TOKEN",
		"POSTMARK_FROM_EMAIL",
		"SLACK_WEBHOOK_URL",
		"DISCORD_WEBHOOK_URL",
		"SES_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
	}
	loaded := 0
	for _, k := range keys {
		if v := data[k]; v != "" {
			os.Setenv(k, v)
			loaded++
		}
	}
	if loaded > 0 {
		log.Info("loaded secret from vault", "path", "capsy/providers/notification", "keys", loaded)
	}
}

func newStore() (store.Store, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return store.NewPostgres(dsn)
	}
	return store.NewFake(), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
