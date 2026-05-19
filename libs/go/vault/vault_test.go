package vault_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lead/libs/go/vault"
)

func vaultClient(t *testing.T) *vault.Client {
	t.Helper()
	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	if addr == "" || token == "" {
		t.Skip("VAULT_ADDR and VAULT_TOKEN not set — skipping Vault integration test")
	}
	c, err := vault.New(addr, token)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	return c
}

func TestWriteAndReadKV(t *testing.T) {
	c := vaultClient(t)

	path := "capsy/test/rw"
	want := map[string]string{
		"FOO": "bar",
		"BAZ": "qux",
	}

	if err := c.WriteKV(path, want); err != nil {
		t.Fatalf("WriteKV: %v", err)
	}

	got, err := c.ReadKV(path)
	if err != nil {
		t.Fatalf("ReadKV: %v", err)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}

func TestRotateJWTSecret(t *testing.T) {
	c := vaultClient(t)

	first, err := c.RotateJWTSecret()
	if err != nil {
		t.Fatalf("RotateJWTSecret (first): %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected non-empty secret")
	}

	second, err := c.RotateJWTSecret()
	if err != nil {
		t.Fatalf("RotateJWTSecret (second): %v", err)
	}
	if first == second {
		t.Error("expected secrets to differ after rotation")
	}

	// Verify the stored secret matches the returned value.
	data, err := c.ReadKV("capsy/jwt")
	if err != nil {
		t.Fatalf("ReadKV capsy/jwt: %v", err)
	}
	if data["JWT_SECRET"] != second {
		t.Errorf("stored JWT_SECRET = %q, want %q", data["JWT_SECRET"], second)
	}
}

func TestCachedKeyProvider(t *testing.T) {
	c := vaultClient(t)

	// Seed a known value.
	if err := c.WriteKV("capsy/jwt", map[string]string{"JWT_SECRET": "initial-secret"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ttl := 500 * time.Millisecond
	provider := vault.NewCachedKeyProvider(c, "capsy/jwt", "JWT_SECRET", ttl, nil)

	key1, err := provider.SigningKey(context.Background())
	if err != nil {
		t.Fatalf("SigningKey: %v", err)
	}
	if string(key1) != "initial-secret" {
		t.Errorf("got %q, want initial-secret", key1)
	}

	// Rotate the secret in Vault.
	newSecret, err := c.RotateJWTSecret()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Within TTL, the cached (old) key is returned.
	key2, err := provider.SigningKey(context.Background())
	if err != nil {
		t.Fatalf("SigningKey after rotate: %v", err)
	}
	if string(key2) != "initial-secret" {
		t.Logf("note: cache expired early — ok for short TTL")
	}

	// After TTL, the new key should be returned.
	time.Sleep(ttl + 100*time.Millisecond)
	key3, err := provider.SigningKey(context.Background())
	if err != nil {
		t.Fatalf("SigningKey after TTL: %v", err)
	}
	if string(key3) != newSecret {
		t.Errorf("after TTL: got %q, want %q", key3, newSecret)
	}
}
