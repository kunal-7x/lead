package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin Qdrant REST client (no gRPC dep). Per-call deadlines come
// from the caller's context. Set a default fallback via WithTimeout.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type Option func(*Client)

func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

func New(url, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(url, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type Point struct {
	ID      string         `json:"id"` // UUID string
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload,omitempty"`
}

type SearchHit struct {
	ID      string         `json:"id"`
	Score   float32        `json:"score"`
	Payload map[string]any `json:"payload"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("qdrant marshal: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant %s %s status=%d body=%s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("qdrant decode: %w body=%s", err, string(respBody))
		}
	}
	return nil
}

// Healthy pings the cluster (10s deadline).
func (c *Client) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// /collections is a cheap, always-available endpoint.
	return c.do(ctx, http.MethodGet, "/collections", nil, nil)
}

// EnsureCollection creates the collection if missing. Idempotent.
func (c *Client) EnsureCollection(ctx context.Context, name string, dim int) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Check existence first.
	var existing struct {
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	err := c.do(ctx, http.MethodGet, "/collections/"+name, nil, &existing)
	if err == nil {
		return nil
	}
	// Create.
	body := map[string]any{
		"vectors": map[string]any{
			"size":     dim,
			"distance": "Cosine",
		},
	}
	if err := c.do(ctx, http.MethodPut, "/collections/"+name, body, nil); err != nil {
		return fmt.Errorf("create collection %s: %w", name, err)
	}
	// Index payload fields for filtering performance.
	for _, field := range []string{"project_id", "kb_version_id", "source_type", "tenant_id"} {
		idx := map[string]any{
			"field_name":   field,
			"field_schema": "keyword",
		}
		_ = c.do(ctx, http.MethodPut, "/collections/"+name+"/index?wait=true", idx, nil)
	}
	return nil
}

// Upsert inserts/updates points. Synchronous (wait=true) so callers can read
// immediately after.
func (c *Client) Upsert(ctx context.Context, collection string, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body := map[string]any{"points": points}
	return c.do(ctx, http.MethodPut, "/collections/"+collection+"/points?wait=true", body, nil)
}

// DeleteByFilter removes points matching the filter (used when re-indexing a
// version).
func (c *Client) DeleteByFilter(ctx context.Context, collection string, filter map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body := map[string]any{"filter": filter}
	return c.do(ctx, http.MethodPost, "/collections/"+collection+"/points/delete?wait=true", body, nil)
}

// Search returns top-K nearest points; filter restricts payload fields.
func (c *Client) Search(ctx context.Context, collection string, vec []float32, topK int, filter map[string]any) ([]SearchHit, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body := map[string]any{
		"vector":       vec,
		"limit":        topK,
		"with_payload": true,
	}
	if filter != nil {
		body["filter"] = filter
	}
	var out struct {
		Result []SearchHit `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, "/collections/"+collection+"/points/search", body, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

// CollectionFor returns the per-tenant collection name.
func CollectionFor(tenantID string) string {
	if tenantID == "" {
		tenantID = "default"
	}
	return "kb_" + tenantID
}

// MatchKeyword builds the Qdrant filter clause for an exact keyword match.
func MatchKeyword(field, value string) map[string]any {
	return map[string]any{
		"key":   field,
		"match": map[string]any{"value": value},
	}
}
