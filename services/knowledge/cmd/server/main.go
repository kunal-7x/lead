package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/handler"
	"github.com/lead/services/knowledge/internal/netutil"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
)

func main() {
	netutil.MaybeOverrideDefaultResolver()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8110"
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

	h := handler.New(s)

	qdrantURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qdrantURL != "" {
		embedder, err := embed.FromEnv()
		if err != nil {
			log.Fatalf("embedding backend: %v", err)
		}
		log.Printf("knowledge: embedding backend = %s (dim=%d)", embedder.Name(), embedder.Dim())
		q := vector.New(qdrantURL, os.Getenv("QDRANT_API_KEY"))
		probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := q.Healthy(probeCtx); err != nil {
			cancel()
			log.Fatalf("qdrant unreachable: %v", err)
		}
		cancel()
		log.Printf("knowledge: qdrant connected at %s", qdrantURL)
		h = h.WithVector(embedder, q)
	} else {
		log.Printf("knowledge: QDRANT_URL not set — running with in-Postgres cosine retrieval (demo mode)")
	}

	log.Printf("knowledge service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h.Router()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
