package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lead/libs/go/events"
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
	} else if demoMode() {
		log.Printf("knowledge: DEMO_MODE=1, using in-memory store")
		s = store.NewFake()
	} else {
		log.Fatal("knowledge: DATABASE_URL is required outside DEMO_MODE")
	}

	h := handler.New(s)
	pub, err := newPublisher()
	if err != nil {
		log.Fatalf("claim publisher: %v", err)
	}
	if pub != nil {
		h = h.WithPublisher(pub)
	}

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
	} else if demoMode() {
		log.Printf("knowledge: QDRANT_URL not set, running with in-Postgres cosine retrieval (demo mode)")
	} else {
		log.Fatal("knowledge: QDRANT_URL is required outside DEMO_MODE")
	}

	log.Printf("knowledge service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h.Router()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func demoMode() bool {
	return os.Getenv("DEMO_MODE") == "1"
}

func newPublisher() (events.Publisher, error) {
	natsURL := strings.TrimSpace(os.Getenv("NATS_URL"))
	if natsURL == "" {
		if demoMode() {
			return nil, nil
		}
		return nil, fmt.Errorf("NATS_URL is required outside DEMO_MODE")
	}
	return events.NewFromEnv(natsURL)
}
