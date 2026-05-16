package vault

import (
	"context"
	"os"
)

// KeyProvider provides signing keys for JWTs.
type KeyProvider interface {
	SigningKey(ctx context.Context) ([]byte, error)
}

// Static is a KeyProvider backed by an env var or explicit secret.
// Used in local dev and tests; in production use VaultProvider.
type Static struct {
	key []byte
}

// NewStatic returns a Static provider. If secret is empty it reads JWT_SECRET env var.
func NewStatic(secret string) *Static {
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}
	if secret == "" {
		secret = "dev-secret-change-in-production"
	}
	return &Static{key: []byte(secret)}
}

func (s *Static) SigningKey(_ context.Context) ([]byte, error) {
	return s.key, nil
}
