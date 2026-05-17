package main

import (
	"log"
	"net/http"
	"os"

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
		log.Printf("DATABASE_URL set but postgres store not implemented; using fake store")
	}
	s = store.NewFake()

	h := handler.New(s)

	log.Printf("campaign service listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Router()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
