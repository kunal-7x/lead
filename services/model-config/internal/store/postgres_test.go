//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresStoreModelConfigRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	key := "tenant." + testID("cfg")
	if err := st.Set(ctx, key, "groq"); err != nil {
		t.Fatalf("set: %v", err)
	}
	value, ok, err := st.Get(ctx, key)
	if err != nil || !ok || value != "groq" {
		t.Fatalf("get value=%q ok=%v err=%v", value, ok, err)
	}
	keys, err := st.Keys(ctx, "tenant.")
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if keys[key] != "groq" {
		t.Fatalf("keys[%q]=%q", key, keys[key])
	}
	if err := st.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func newTestPostgres(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	st, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	return st
}

func testID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
