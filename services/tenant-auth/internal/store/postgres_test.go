//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresStoreTenantAuthRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenant, err := st.CreateTenant(ctx, "Tenant "+testID("ta"), "in")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	got, err := st.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if got.ID != tenant.ID {
		t.Fatalf("tenant id=%q want %q", got.ID, tenant.ID)
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
