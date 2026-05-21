package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lead/services/analytics-sink/internal/dimrefresh"
	"github.com/lead/services/analytics-sink/internal/handler"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := newStore()
	if err != nil {
		log.Error("store init", "error", err)
		os.Exit(1)
	}
	if closer, ok := st.(interface{ Close() }); ok {
		defer closer.Close()
	}
	svc := service.New(st)

	if chStore, ok := st.(*store.ClickHouseStore); ok {
		if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
			refresher, err := dimrefresh.New(ctx, dsn, chStore.Client())
			if err != nil {
				log.Warn("analytics-sink: dim refresher disabled", "error", err)
			} else {
				defer refresher.Close()
				go refresher.Run(ctx, 5*time.Minute, log)
			}
		}
	}

	go startConsumer(ctx, log, svc)

	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)
	addr := envOr("ANALYTICS_SINK_ADDR", ":8116")
	log.Info("analytics-sink listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func newStore() (store.Store, error) {
	switch os.Getenv("ANALYTICS_STORE") {
	case "fake":
		return store.NewFake(), nil
	case "postgres":
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return nil, errEnvRequired("DATABASE_URL", "ANALYTICS_STORE=postgres")
		}
		return store.NewPostgres(dsn)
	}
	if os.Getenv("DEMO_MODE") == "1" {
		return store.NewFake(), nil
	}
	return store.NewClickHouse(envOr("CLICKHOUSE_URL", "http://localhost:8123"))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func errEnvRequired(key, context string) error {
	return &envRequiredError{key: key, context: context}
}

type envRequiredError struct {
	key     string
	context string
}

func (e *envRequiredError) Error() string {
	return e.key + " is required when " + e.context
}
