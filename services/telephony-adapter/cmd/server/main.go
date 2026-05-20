package main

import (
	"log"
	"net/http"
	"os"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/handler"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

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

	wh := webhook.New(plivoAuthToken, s, pub)
	r := routing.New(s, adapters)
	h := handler.New(wh, r)

	log.Printf("telephony-adapter listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatal(err)
	}
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
