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

// HuggingFace Inference API embedding provider. Model must be set via
// HUGGINGFACE_EMBED_MODEL (e.g. sentence-transformers/all-MiniLM-L6-v2 = 384
// dim, BAAI/bge-base-en-v1.5 = 768 dim). Default = 768-dim bge-base to match
// Gemini's 768.
type HuggingFace struct {
	apiKey string
	model  string
	dim    int
	http   *http.Client
}

func NewHuggingFace(apiKey string) *HuggingFace {
	model := os.Getenv("HUGGINGFACE_EMBED_MODEL")
	if model == "" {
		model = "BAAI/bge-base-en-v1.5"
	}
	return &HuggingFace{
		apiKey: apiKey,
		model:  model,
		dim:    dimFromEnv("HUGGINGFACE_EMBED_DIM", 768),
		http:   &http.Client{Timeout: 25 * time.Second}, // HF cold-start can be slow
	}
}

func (h *HuggingFace) Name() string { return "hf-" + h.model }
func (h *HuggingFace) Dim() int     { return h.dim }

func (h *HuggingFace) Embed(ctx context.Context, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	body := map[string]any{"inputs": text}
	b, _ := json.Marshal(body)
	url := "https://api-inference.huggingface.co/pipeline/feature-extraction/" + h.model
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hf embed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hf embed status=%d body=%s", resp.StatusCode, string(raw))
	}
	// HF returns either a flat []float64 (single input) or a wrapped form.
	var flat []float32
	if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 {
		return flat, nil
	}
	var nested [][]float32
	if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 && len(nested[0]) > 0 {
		return nested[0], nil
	}
	return nil, fmt.Errorf("hf unexpected embed payload: %s", string(raw))
}
