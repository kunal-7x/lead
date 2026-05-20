package index

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
)

// Indexer chunks + embeds + upserts a KB version to Qdrant. Built once at
// service startup; safe for concurrent use.
type Indexer struct {
	store    store.Store
	embedder embed.Provider
	qdrant   *vector.Client
}

func New(s store.Store, e embed.Provider, q *vector.Client) *Indexer {
	return &Indexer{store: s, embedder: e, qdrant: q}
}

// IndexVersion fetches all chunks for the version, embeds them, and upserts
// into the tenant's collection. If chunks for this version already exist they
// are replaced (delete-then-upsert by filter).
func (i *Indexer) IndexVersion(ctx context.Context, tenantID, projectID, versionID string) (int, error) {
	if i == nil || i.qdrant == nil || i.embedder == nil {
		return 0, fmt.Errorf("indexer not configured")
	}

	collection := vector.CollectionFor(tenantID)
	if err := i.qdrant.EnsureCollection(ctx, collection, i.embedder.Dim()); err != nil {
		return 0, fmt.Errorf("ensure collection: %w", err)
	}

	// Drop any prior points for this version so re-publish replaces cleanly.
	delFilter := map[string]any{
		"must": []map[string]any{
			vector.MatchKeyword("kb_version_id", versionID),
		},
	}
	if err := i.qdrant.DeleteByFilter(ctx, collection, delFilter); err != nil {
		log.Printf("indexer: delete prior points (non-fatal): %v", err)
	}

	type chunk struct {
		id         string
		sourceType string
		text       string
	}
	var chunks []chunk

	facts, err := i.store.ListFactsByVersion(ctx, versionID)
	if err != nil {
		return 0, fmt.Errorf("list facts: %w", err)
	}
	for _, f := range facts {
		for j, piece := range chunkText(f.Content, 512, 128) {
			chunks = append(chunks, chunk{
				id:         pointID(f.ID, j),
				sourceType: "fact",
				text:       piece,
			})
		}
	}

	faqs, err := i.store.ListFAQsByVersion(ctx, versionID)
	if err != nil {
		return 0, fmt.Errorf("list faqs: %w", err)
	}
	for _, q := range faqs {
		combined := "Q: " + q.Question + "\nA: " + q.Answer
		for j, piece := range chunkText(combined, 512, 128) {
			chunks = append(chunks, chunk{
				id:         pointID(q.ID, j),
				sourceType: "faq",
				text:       piece,
			})
		}
	}

	discs, err := i.store.ListDisclaimersByVersion(ctx, versionID)
	if err != nil {
		return 0, fmt.Errorf("list disclaimers: %w", err)
	}
	for _, d := range discs {
		for j, piece := range chunkText(d.Text, 512, 128) {
			chunks = append(chunks, chunk{
				id:         pointID(d.ID, j),
				sourceType: "disclaimer",
				text:       piece,
			})
		}
	}

	if len(chunks) == 0 {
		return 0, nil
	}

	// Embed sequentially to keep request rate predictable and avoid free-tier
	// rate limits. 1-2 chunks/sec is fine for v1.
	points := make([]vector.Point, 0, len(chunks))
	for _, c := range chunks {
		embedCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		vec, err := i.embedder.Embed(embedCtx, c.text)
		cancel()
		if err != nil {
			return len(points), fmt.Errorf("embed chunk %s (%s): %w", c.id, i.embedder.Name(), err)
		}
		points = append(points, vector.Point{
			ID:     c.id,
			Vector: vec,
			Payload: map[string]any{
				"tenant_id":     tenantID,
				"project_id":    projectID,
				"kb_version_id": versionID,
				"source_type":   c.sourceType,
				"text":          c.text,
			},
		})
	}

	// Upsert in batches of 64 to stay under any payload size limits.
	const batch = 64
	for start := 0; start < len(points); start += batch {
		end := start + batch
		if end > len(points) {
			end = len(points)
		}
		if err := i.qdrant.Upsert(ctx, collection, points[start:end]); err != nil {
			return start, fmt.Errorf("upsert batch %d..%d: %w", start, end, err)
		}
	}
	return len(points), nil
}

// chunkText is a simple word-window splitter. tokens ~= words for English; for
// Indic mixed content this overshoots which is fine.
func chunkText(text string, windowWords, overlapWords int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	if len(words) <= windowWords {
		return []string{text}
	}
	var chunks []string
	step := windowWords - overlapWords
	if step <= 0 {
		step = windowWords
	}
	for i := 0; i < len(words); i += step {
		end := i + windowWords
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
		if end == len(words) {
			break
		}
	}
	return chunks
}

// pointID derives a deterministic UUID-shaped ID per chunk so re-publishing
// the same source produces idempotent updates. Qdrant accepts uuid strings; we
// build one from the source row ID + chunk index.
func pointID(sourceID string, chunkIdx int) string {
	if chunkIdx == 0 {
		return sourceID
	}
	// Replace the last hex chunk of the UUID with the chunk index so the ID
	// remains a valid UUID v4 layout. Cheaper than rehashing.
	if len(sourceID) >= 36 {
		suffix := fmt.Sprintf("%012x", chunkIdx)
		return sourceID[:24] + suffix
	}
	return sourceID + "-" + fmt.Sprintf("%d", chunkIdx)
}
