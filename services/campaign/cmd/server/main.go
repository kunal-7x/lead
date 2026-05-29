package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/campaign/internal/handler"
	"github.com/lead/services/campaign/internal/store"
)

func main() {
	addr := os.Getenv("CAMPAIGN_ADDR")
	if addr == "" {
		addr = ":8112"
	}

	var s store.Store
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err := store.NewPostgres(dsn)
		if err != nil {
			log.Fatalf("connect postgres: %v", err)
		}
		defer pg.Close()
		s = pg
	} else {
		s = store.NewFake()
	}

	pub, err := events.NewFromEnv(os.Getenv("NATS_URL"))
	if err != nil {
		log.Fatalf("campaign: connect NATS: %v", err)
	}
	defer pub.Close()
	if os.Getenv("NATS_URL") != "" {
		log.Printf("campaign: loaded publisher from JetStream %s", os.Getenv("NATS_URL"))
	}

	llmRouterURL := os.Getenv("LLM_ROUTER_URL")
	if llmRouterURL == "" {
		llmRouterURL = "http://llm-router:8111"
	}
	h := handler.NewWithLLMRouter(s, llmRouterURL)

	// Wrap the campaign handler to emit campaign.launched events after launch.
	mux := http.NewServeMux()
	mux.Handle("/", h.Router())

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Override the launch endpoint to also publish the event.
	base := h.Router()
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && isLaunchPath(r.URL.Path) {
			rw := &captureWriter{ResponseWriter: w}
			base.ServeHTTP(rw, r)
			if rw.status == http.StatusOK {
				campaignID := launchPathID(r.URL.Path)
				if campaignID != "" {
					publishLaunched(r.Context(), pub, campaignID)
				}
			}
			return
		}
		base.ServeHTTP(w, r)
	})

	_ = ctx

	log.Printf("campaign service listening on %s", addr)
	if err := http.ListenAndServe(addr, wrapped); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func publishLaunched(ctx context.Context, pub events.Publisher, campaignID string) {
	data, _ := json.Marshal(map[string]string{"campaign_id": campaignID})
	if err := pub.Publish(ctx, events.SubjectCampaignLaunched, data, events.WithIdempotencyKey("launch-"+campaignID)); err != nil {
		log.Printf("campaign: publish launched: %v", err)
	}
}

type captureWriter struct {
	http.ResponseWriter
	status int
}

func (cw *captureWriter) WriteHeader(status int) {
	cw.status = status
	cw.ResponseWriter.WriteHeader(status)
}

func isLaunchPath(path string) bool {
	// matches /v1/campaigns/{id}/launch
	parts := splitPath(path)
	return len(parts) == 4 && parts[0] == "v1" && parts[1] == "campaigns" && parts[3] == "launch"
}

func launchPathID(path string) string {
	parts := splitPath(path)
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i, c := range path {
		if c == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		parts = append(parts, path[start:])
	}
	return parts
}
