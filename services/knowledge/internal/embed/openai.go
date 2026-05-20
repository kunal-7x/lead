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

// OpenAI uses text-embedding-3-small. Native dim 1536; can request lower via
// "dimensions" param. To match Gemini's 768 default, pass EMBEDDING_DIM=768.
type OpenAI struct {
	apiKey string
	model  string
	dim    int
	http   *http.Client
}

func NewOpenAI(apiKey string) *OpenAI {
	model := os.Getenv("OPENAI_EMBED_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAI{
		apiKey: apiKey,
		model:  model,
		dim:    dimFromEnv("OPENAI_EMBED_DIM", 768),
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (o *OpenAI) Name() string { return "openai-" + o.model }
func (o *OpenAI) Dim() int     { return o.dim }

func (o *OpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body := map[string]any{
		"input":      text,
		"model":      o.model,
		"dimensions": o.dim,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai embed status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("openai decode: %w body=%s", err, string(raw))
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("openai empty embedding body=%s", string(raw))
	}
	return out.Data[0].Embedding, nil
}
