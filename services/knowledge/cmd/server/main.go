package main

import (
	"log"
	"net/http"
	"os"

	"github.com/lead/services/knowledge/internal/handler"
	"github.com/lead/services/knowledge/internal/store"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Use fake store unless DATABASE_URL is set.
	// In production, swap this for a real postgres store.
	var s store.Store
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		log.Printf("DATABASE_URL set but postgres store not implemented; using fake store")
	}
	s = store.NewFake()

	h := handler.New(s)

	log.Printf("knowledge service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h.Router()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
