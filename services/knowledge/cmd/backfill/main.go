// backfill_kb re-embeds every currently-published KB version and re-upserts
// into Qdrant. Run when switching embedding backends or after a Qdrant wipe.
//
// Usage:
//
//	go run ./cmd/backfill
//
// Reads env: DATABASE_URL, QDRANT_URL, QDRANT_API_KEY, GOOGLE_GEMINI_API_KEY
// (or OPENAI_API_KEY / HUGGINGFACE_TOKEN), EMBEDDING_BACKEND, CUSTOM_DNS.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/index"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/netutil"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
)

func main() {
	netutil.MaybeOverrideDefaultResolver()

	projectFilter := flag.String("project", "", "only re-index this project ID (default: all published)")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	qdrantURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qdrantURL == "" {
		log.Fatal("QDRANT_URL is required")
	}

	pg, err := store.NewPostgres(dsn)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pg.Close()

	embedder, err := embed.FromEnv()
	if err != nil {
		log.Fatalf("embedder: %v", err)
	}
	q := vector.New(qdrantURL, os.Getenv("QDRANT_API_KEY"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := q.Healthy(ctx); err != nil {
		cancel()
		log.Fatalf("qdrant: %v", err)
	}
	cancel()

	idx := index.New(pg, embedder, q)

	bg := context.Background()
	var projects []*model.Project
	if *projectFilter != "" {
		p, err := pg.GetProject(bg, *projectFilter)
		if err != nil {
			log.Fatalf("project %s: %v", *projectFilter, err)
		}
		projects = []*model.Project{p}
	} else {
		// List across all tenants by iterating distinct tenant IDs is overkill
		// for v1: just walk every project row. ListProjects requires tenant.
		// Add a helper later if needed.
		log.Fatal("listing across all tenants not implemented; pass -project <id>")
	}

	for _, p := range projects {
		if p.ActiveVersionID == "" {
			log.Printf("skip project %s: no active version", p.ID)
			continue
		}
		start := time.Now()
		n, err := idx.IndexVersion(bg, p.TenantID, p.ID, p.ActiveVersionID)
		if err != nil {
			log.Printf("FAIL project=%s version=%s wrote=%d err=%v", p.ID, p.ActiveVersionID, n, err)
			continue
		}
		log.Printf("OK   project=%s version=%s points=%d in %s", p.ID, p.ActiveVersionID, n, time.Since(start))
	}
}
