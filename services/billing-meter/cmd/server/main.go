package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/lead/services/billing-meter/internal/handler"
	"github.com/lead/services/billing-meter/internal/service"
	"github.com/lead/services/billing-meter/internal/store"
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
	svc := service.New(st)
	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)
	addr := envOr("BILLING_METER_ADDR", ":8115")
	log.Info("billing-meter listening", "addr", addr)
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
