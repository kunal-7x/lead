package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/call-intel/internal/consumer"
	"github.com/lead/services/call-intel/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		log.Error("NATS_URL is required")
		os.Exit(1)
	}

	js, err := events.New(natsURL)
	if err != nil {
		log.Error("NATS connect failed", "err", err)
		os.Exit(1)
	}
	defer js.Close()

	// Store is optional — when DATABASE_URL is absent, transcript persist skips.
	var st *store.Store
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		s, err := store.New(context.Background(), dsn)
		if err != nil {
			log.Error("store init failed", "err", err)
			os.Exit(1)
		}
		defer s.Close()
		st = s
		log.Info("transcript store: postgres")
	} else {
		log.Warn("DATABASE_URL not set; transcript persistence disabled")
	}

	cfg := consumer.Config{
		NatsURL:       natsURL,
		ScoringURL:    envOr("SCORING_URL", ""),
		LeadImportURL: envOr("LEAD_IMPORT_URL", ""),
		KnowledgeURL:  envOr("KNOWLEDGE_URL", ""),
		SiteVisitURL:  envOr("SITE_VISIT_URL", ""),
		HandoffURL:    envOr("HANDOFF_URL", ""),
		SchedulerURL:  envOr("SCHEDULER_URL", ""),
	}

	log.Info("call-intel consumer starting",
		"scoring_url", cfg.ScoringURL,
		"lead_import_url", cfg.LeadImportURL,
		"knowledge_url", cfg.KnowledgeURL,
		"site_visit_url", cfg.SiteVisitURL,
		"handoff_url", cfg.HandoffURL,
		"scheduler_url", cfg.SchedulerURL,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	r := consumer.New(cfg, st, js, log)
	go r.Run(ctx)

	// Minimal health endpoint.
	addr := envOr("CALL_INTEL_ADDR", ":8119")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Info("call-intel healthz listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("healthz server error", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("call-intel shutting down")
	_ = srv.Close()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
