package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/scheduler/internal/dispatcher"
	"github.com/lead/services/scheduler/internal/handler"
	"github.com/lead/services/scheduler/internal/picker"
	"github.com/lead/services/scheduler/internal/store"
)

func main() {
	addr := os.Getenv("SCHEDULER_ADDR")
	if addr == "" {
		addr = ":8107"
	}

	s, err := newStore()
	if err != nil {
		log.Fatalf("connect store: %v", err)
	}
	if closer, ok := s.(interface{ Close() }); ok {
		defer closer.Close()
	}
	p := picker.New(s)
	h := handler.NewWithTelephony(p, envOr("TELEPHONY_URL", "http://localhost:8108"), http.DefaultClient)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Wire the campaign dispatcher if NATS + Redis are configured.
	startDispatcher(ctx)

	log.Printf("scheduler listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatal(err)
	}
}

// startDispatcher wires the Dispatcher when required infra env vars are set.
// If NATS_URL or REDIS_URL is unset, it logs a warning and skips — existing
// tests and dev envs without infra still boot normally.
func startDispatcher(ctx context.Context) {
	natsURL := os.Getenv("NATS_URL")
	redisURL := os.Getenv("REDIS_URL")

	if natsURL == "" || redisURL == "" {
		log.Printf("scheduler: NATS_URL or REDIS_URL not set; dispatcher disabled")
		return
	}

	var sub dispatcher.Subscriber
	js, err := events.New(natsURL)
	if err != nil {
		log.Printf("scheduler: NATS connect error (%v); dispatcher disabled", err)
		return
	}
	sub = dispatcher.NewNATSSubscriber(js)

	rw := dispatcher.NewRedisWriterTCP(redisURL)
	q := dispatcher.NewFakeDialQueueStore() // Phase C will swap in a Postgres-backed store
	cfg := dispatcher.Config{
		CampaignURL:          envOr("CAMPAIGN_URL", "http://localhost:8112"),
		LeadImportURL:        envOr("LEAD_IMPORT_URL", "http://localhost:8110"),
		TelephonyURL:         envOr("TELEPHONY_URL", "http://localhost:8108"),
		FromNumber:           os.Getenv("FROM_NUMBER"),
		PublicWebhookBaseURL: os.Getenv("PUBLIC_WEBHOOK_BASE_URL"),
	}

	d := dispatcher.New(sub, rw, http.DefaultClient, q, cfg)
	go d.Subscribe(ctx)
	go d.Run(ctx)
	log.Printf("scheduler: dispatcher started (NATS=%s)", natsURL)
}

func newStore() (store.Store, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return store.NewPostgres(dsn)
	}
	return store.NewFake(), nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
