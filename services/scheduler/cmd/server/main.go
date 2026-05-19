package main

import (
	"log"
	"net/http"
	"os"

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

	log.Printf("scheduler listening on %s", addr)
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

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
