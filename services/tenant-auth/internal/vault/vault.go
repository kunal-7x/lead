package vault

import (
	"context"
	"os"
	"time"

	libsvault "github.com/lead/libs/go/vault"
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

// NewVaultProvider returns a KeyProvider that reads JWT_SECRET from Vault
// at secret/data/capsy/jwt, caching for 5 seconds. On failure it falls back
// to the static secret so dev without Vault keeps working.
func NewVaultProvider(vc *libsvault.Client, staticSecret string) KeyProvider {
	if staticSecret == "" {
		staticSecret = os.Getenv("JWT_SECRET")
	}
	if staticSecret == "" {
		staticSecret = "dev-secret-change-in-production"
	}
	return libsvault.NewCachedKeyProvider(vc, "capsy/jwt", "JWT_SECRET",
		5*time.Second, []byte(staticSecret))
}
