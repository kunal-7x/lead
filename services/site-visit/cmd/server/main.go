package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/lead/services/site-visit/internal/handler"
	"github.com/lead/services/site-visit/internal/service"
	"github.com/lead/services/site-visit/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	svc := service.New(store.NewFake())
	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)

	addr := envOr("SITE_VISIT_ADDR", ":8114")
	log.Info("site-visit listening", "addr", addr)
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
