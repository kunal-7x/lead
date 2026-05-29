package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/handler"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

// envFallback returns the first non-empty value among the given env var names.
func envFallback(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

func main() {
	addr := os.Getenv("TELEPHONY_ADDR")
	if addr == "" {
		addr = ":8108"
	}

	plivoAuthID := os.Getenv("PLIVO_AUTH_ID")
	plivoAuthToken := os.Getenv("PLIVO_AUTH_TOKEN")

	s, err := newStore()
	if err != nil {
		log.Fatalf("connect store: %v", err)
	}
	if closer, ok := s.(interface{ Close() }); ok {
		defer closer.Close()
	}

	pub := newPublisher()

	adapters := []adapter.Telephony{
		adapter.NewExotel(),
		adapter.NewTwilio(),
		adapter.NewMock(),
	}
	// Register real Plivo adapter when credentials are present. With both
	// PLIVO_AUTH_ID and PLIVO_AUTH_TOKEN set, outbound /v1/calls routed to
	// provider "plivo" hit the live Plivo REST API.
	if plivoAuthID != "" && plivoAuthToken != "" {
		plivoHTTP := adapter.NewPlivoHTTP(nil)
		adapters = append(adapters, adapter.NewPlivo(plivoAuthID, plivoAuthToken, plivoHTTP))
		log.Printf("telephony-adapter: registered live Plivo adapter (auth_id=%s)", maskAuthID(plivoAuthID))
	} else {
		log.Println("telephony-adapter: PLIVO_AUTH_ID/TOKEN not set; Plivo adapter NOT registered")
	}

	// Register Vobiz adapter when VOBIZ_AUTH_ID+VOBIZ_AUTH_TOKEN are set.
	// Env var precedence: VOBIZ_AUTH_ID > VOBIZ_API_KEY, VOBIZ_AUTH_TOKEN > VOBIZ_API_SECRET.
	// VOBIZ_BASE_URL overrides the default https://api.vobiz.ai/api/v1.
	vobizAuthID := envFallback("VOBIZ_AUTH_ID", "VOBIZ_API_KEY")
	vobizAuthToken := envFallback("VOBIZ_AUTH_TOKEN", "VOBIZ_API_SECRET")
	if vobizAuthID != "" && vobizAuthToken != "" {
		var vobizHTTP adapter.VobizHTTPClient
		if baseURL := os.Getenv("VOBIZ_BASE_URL"); baseURL != "" {
			vobizHTTP = adapter.NewVobizHTTPWithBase(baseURL, nil)
		} else {
			vobizHTTP = adapter.NewVobizHTTP(nil)
		}
		adapters = append(adapters, adapter.NewVobiz(vobizAuthID, vobizAuthToken, vobizHTTP))
		log.Printf("telephony-adapter: registered live Vobiz adapter (auth_id=%s)", maskAuthID(vobizAuthID))
	} else {
		log.Println("telephony-adapter: VOBIZ_AUTH_ID/TOKEN not set; Vobiz adapter NOT registered")
	}

	wh := webhook.New(plivoAuthToken, s, pub)
	r := routing.New(s, adapters)

	// If CALLING_PROVIDER=vobiz, inject a priority-0 routing rule so Vobiz is
	// preferred over all other providers. The injectVobizRoutingRule function
	// uses a type assertion to call AddRoutingRule on the Fake store (dev/test).
	// In production the rule should be inserted into the database directly.
	if cp := os.Getenv("CALLING_PROVIDER"); cp == "vobiz" {
		if err := injectVobizRoutingRule(s); err != nil {
			log.Printf("telephony-adapter: WARN failed to inject Vobiz routing rule: %v", err)
		} else {
			log.Println("telephony-adapter: CALLING_PROVIDER=vobiz; injected priority-0 routing rule")
		}
	}

	h := handler.New(wh, r)

	log.Printf("telephony-adapter listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatal(err)
	}
}

// injectVobizRoutingRule adds a priority-0 routing rule for Vobiz so it is
// preferred when CALLING_PROVIDER=vobiz. This is only meaningful for the
// in-memory Fake store used in dev/test. Production should manage routing
// rules in the database.
func injectVobizRoutingRule(s store.Store) error {
	return s.UpsertRoutingRule(context.Background(), &model.ProviderRoutingRule{
		ID:          "rule-vobiz-env",
		ProviderID:  "vobiz",
		Priority:    0,
		MaxFailRate: 1.0,
	})
}

func newStore() (store.Store, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return store.NewPostgres(dsn)
	}
	return store.NewFake(), nil
}

func maskAuthID(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}

// newPublisher returns a real JetStream publisher when NATS_URL is set,
// otherwise a Noop publisher so the service runs in dev without NATS.
func newPublisher() outbox.Publisher {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		log.Println("telephony-adapter: NATS_URL not set, using noop publisher")
		return outbox.NewFakePublisher()
	}
	ep, err := events.New(natsURL)
	if err != nil {
		log.Fatalf("telephony-adapter: connect NATS %s: %v", natsURL, err)
	}
	log.Printf("telephony-adapter: loaded publisher from JetStream %s", natsURL)
	return outbox.NewJetStream(ep)
}
