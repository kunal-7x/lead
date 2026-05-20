package embed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Provider produces a normalized vector for a single text input. Backends
// supply their native dimension via Dim().
type Provider interface {
	Name() string
	Dim() int
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Chain wraps several providers and falls back on transient errors.
// The first provider's dimension is treated as canonical — all members must
// return the same dim or we error out at construction.
type Chain struct {
	primary    Provider
	fallbacks  []Provider
	primaryDim int
}

func (c *Chain) Name() string {
	names := []string{c.primary.Name()}
	for _, f := range c.fallbacks {
		names = append(names, f.Name())
	}
	return "chain[" + strings.Join(names, ">") + "]"
}

func (c *Chain) Dim() int { return c.primaryDim }

func (c *Chain) Embed(ctx context.Context, text string) ([]float32, error) {
	v, err := c.primary.Embed(ctx, text)
	if err == nil {
		return v, nil
	}
	lastErr := fmt.Errorf("primary %s: %w", c.primary.Name(), err)
	for _, fb := range c.fallbacks {
		v2, err2 := fb.Embed(ctx, text)
		if err2 == nil {
			return v2, nil
		}
		lastErr = fmt.Errorf("%v; fallback %s: %w", lastErr, fb.Name(), err2)
	}
	return nil, lastErr
}

// FromEnv constructs the embedding chain from EMBEDDING_BACKEND and available
// API keys. Order:
//   - EMBEDDING_BACKEND explicit choice (gemini|openai|hf) → primary
//   - All other providers with credentials become fallbacks
//
// Dimension is fixed to the primary's native dim. Fallbacks that don't match
// are dropped (with a warning printed).
func FromEnv() (Provider, error) {
	choice := strings.ToLower(strings.TrimSpace(os.Getenv("EMBEDDING_BACKEND")))
	geminiKey := strings.TrimSpace(os.Getenv("GOOGLE_GEMINI_API_KEY"))
	openaiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	hfKey := strings.TrimSpace(os.Getenv("HUGGINGFACE_TOKEN"))

	providers := map[string]Provider{}
	if geminiKey != "" {
		providers["gemini"] = NewGemini(geminiKey)
	}
	if openaiKey != "" {
		providers["openai"] = NewOpenAI(openaiKey)
	}
	if hfKey != "" {
		providers["hf"] = NewHuggingFace(hfKey)
	}
	if len(providers) == 0 {
		return nil, errors.New("no embedding provider configured: set GOOGLE_GEMINI_API_KEY, OPENAI_API_KEY, or HUGGINGFACE_TOKEN")
	}

	if choice == "" {
		// Default order: gemini (user-provided key works) → openai → hf
		for _, c := range []string{"gemini", "openai", "hf"} {
			if _, ok := providers[c]; ok {
				choice = c
				break
			}
		}
	}
	primary, ok := providers[choice]
	if !ok {
		return nil, fmt.Errorf("EMBEDDING_BACKEND=%q but no credentials for that backend", choice)
	}
	delete(providers, choice)

	var fallbacks []Provider
	// Stable order: gemini, openai, hf (excluding primary).
	for _, c := range []string{"gemini", "openai", "hf"} {
		if p, ok := providers[c]; ok && p.Dim() == primary.Dim() {
			fallbacks = append(fallbacks, p)
		}
	}
	return &Chain{primary: primary, fallbacks: fallbacks, primaryDim: primary.Dim()}, nil
}

// DimFromEnv allows overriding dim for Gemini (which supports 768/1536/3072 via
// outputDimensionality).
func dimFromEnv(envKey string, fallback int) int {
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
