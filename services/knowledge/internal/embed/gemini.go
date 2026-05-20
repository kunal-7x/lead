package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Gemini uses text-embedding-004 (768 dims by default). Endpoint:
//
//	POST https://generativelanguage.googleapis.com/v1beta/models/text-embedding-004:embedContent?key=$KEY
type Gemini struct {
	apiKey string
	model  string
	dim    int
	http   *http.Client
}

func NewGemini(apiKey string) *Gemini {
	model := os.Getenv("GEMINI_EMBED_MODEL")
	if model == "" {
		model = "gemini-embedding-001"
	}
	return &Gemini{
		apiKey: apiKey,
		model:  model,
		dim:    dimFromEnv("GEMINI_EMBED_DIM", 768),
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *Gemini) Name() string { return "gemini-" + g.model }
func (g *Gemini) Dim() int     { return g.dim }

func (g *Gemini) Embed(ctx context.Context, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body := map[string]any{
		"model": "models/" + g.model,
		"content": map[string]any{
			"parts": []map[string]string{{"text": text}},
		},
		// Pin output dim explicitly; gemini-embedding-001 supports 768/1536/3072
		// via Matryoshka and defaults to 3072 otherwise.
		"outputDimensionality": g.dim,
	}
	b, _ := json.Marshal(body)
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + g.model + ":embedContent?key=" + g.apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini embed status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gemini decode: %w body=%s", err, string(raw))
	}
	if len(out.Embedding.Values) == 0 {
		return nil, fmt.Errorf("gemini empty embedding body=%s", string(raw))
	}
	return out.Embedding.Values, nil
}
