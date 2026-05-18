package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/lead/services/handoff/internal/handler"
	"github.com/lead/services/handoff/internal/service"
	"github.com/lead/services/handoff/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	st := store.NewFake()
	svc := service.New(st)
	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)

	addr := envOr("HANDOFF_ADDR", ":8112")
	log.Info("handoff listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
