//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresStoreFreeSwitchBridgeRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	instance := Instance{ID: testID("fs"), Host: "localhost", SIPPort: 5060, ESLPort: 8021, Healthy: true}
	if err := st.SaveInstance(ctx, instance); err != nil {
		t.Fatalf("save instance: %v", err)
	}
	instances, err := st.ListInstances(ctx)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(instances) == 0 {
		t.Fatal("instances empty")
	}
	if err := st.SaveUpload(ctx, "tenant", testID("session"), "spaces/key.wav", 42); err != nil {
		t.Fatalf("save upload: %v", err)
	}
	if err := st.SetSessionStarted(ctx, "call-1"); err != nil {
		t.Fatalf("session started: %v", err)
	}
	if err := st.SetSessionEnded(ctx, "call-1"); err != nil {
		t.Fatalf("session ended: %v", err)
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
