package vault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

// Client is a thin wrapper around the HashiCorp Vault API for KV v2 secret reads/writes.
type Client struct {
	c     *vaultapi.Client
	mount string
}

// New creates a Vault client pointing at addr authenticated with token.
func New(addr, token string) (*Client, error) {
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr
	cfg.Timeout = 5 * time.Second
	c, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("vault new client: %w", err)
	}
	c.SetToken(token)
	return &Client{c: c, mount: "secret"}, nil
}

// ReadKV reads a KV v2 secret. path is relative to the mount, e.g. "capsy/jwt".
// Returns a flat map of string values from the secret's data field.
func (c *Client) ReadKV(path string) (map[string]string, error) {
	s, err := c.c.KVv2(c.mount).Get(context.Background(), path)
	if err != nil {
		return nil, fmt.Errorf("vault read %q: %w", path, err)
	}
	if s == nil {
		return nil, fmt.Errorf("vault: no data at %q", path)
	}
	out := make(map[string]string, len(s.Data))
	for k, v := range s.Data {
		if sv, ok := v.(string); ok {
			out[k] = sv
		}
	}
	return out, nil
}

// WriteKV writes a KV v2 secret at path.
func (c *Client) WriteKV(path string, data map[string]string) error {
	m := make(map[string]any, len(data))
	for k, v := range data {
		m[k] = v
	}
	if _, err := c.c.KVv2(c.mount).Put(context.Background(), path, m); err != nil {
		return fmt.Errorf("vault write %q: %w", path, err)
	}
	return nil
}

// RotateJWTSecret generates a new 32-byte random secret, stores it at
// secret/data/capsy/jwt under the key JWT_SECRET, and returns it.
func (c *Client) RotateJWTSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	secret := hex.EncodeToString(b)
	if err := c.WriteKV("capsy/jwt", map[string]string{"JWT_SECRET": secret}); err != nil {
		return "", err
	}
	return secret, nil
}

// CachedKeyProvider is a KeyProvider that reads JWT_SECRET from Vault with a TTL cache.
// It satisfies the same KeyProvider interface used by tenant-auth.
// On cache miss or expiry it fetches from Vault; on Vault error it falls back to the
// cached value (or the provided static fallback on first failure).
type CachedKeyProvider struct {
	vc       *Client
	path     string
	ttl      time.Duration
	fallback []byte

	mu        sync.Mutex
	cached    []byte
	expiresAt time.Time
}

// NewCachedKeyProvider returns a CachedKeyProvider that reads from vault path
// (e.g. "capsy/jwt", field "JWT_SECRET") with the given cache TTL.
// fallback is used when Vault is unavailable and the cache is cold.
func NewCachedKeyProvider(vc *Client, path, field string, ttl time.Duration, fallback []byte) *CachedKeyProvider {
	return &CachedKeyProvider{
		vc:       vc,
		path:     path,
		ttl:      ttl,
		fallback: fallback,
	}
}

// SigningKey returns the current JWT signing key.
func (p *CachedKeyProvider) SigningKey(_ context.Context) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.cached) > 0 && time.Now().Before(p.expiresAt) {
		return p.cached, nil
	}

	// Refresh from Vault.
	data, err := p.vc.ReadKV(p.path)
	if err != nil {
		if len(p.cached) > 0 {
			return p.cached, nil
		}
		if len(p.fallback) > 0 {
			return p.fallback, nil
		}
		return nil, fmt.Errorf("vault key refresh: %w", err)
	}

	// Use the first string value found (caller should set path correctly).
	for _, v := range data {
		if v != "" {
			p.cached = []byte(v)
			p.expiresAt = time.Now().Add(p.ttl)
			return p.cached, nil
		}
	}

	if len(p.cached) > 0 {
		return p.cached, nil
	}
	return nil, fmt.Errorf("vault: no usable key at %q", p.path)
}
