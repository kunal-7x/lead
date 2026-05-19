package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/lead/services/notification/internal/adapters"
	"github.com/lead/services/notification/internal/handler"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
	"github.com/lead/services/notification/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)

	addr := envOr("NOTIFICATION_ADDR", ":8113")
	log.Info("notification listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
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
