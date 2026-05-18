package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/lead/services/model-config/internal/handler"
	"github.com/lead/services/model-config/internal/service"
	"github.com/lead/services/model-config/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	svc := service.New(store.NewFake())
	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)
	addr := envOr("MODEL_CONFIG_ADDR", ":8117")
	log.Info("model-config listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
