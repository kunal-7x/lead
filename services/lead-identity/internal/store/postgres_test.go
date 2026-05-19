//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresStoreContactMerge(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	primary, created, err := st.GetOrCreateContact(ctx, tenantID, "+911111111111", "a@example.test", "A")
	if err != nil || !created {
		t.Fatalf("create primary: created=%v err=%v", created, err)
	}
	duplicate, _, err := st.GetOrCreateContact(ctx, tenantID, "+922222222222", "b@example.test", "B")
	if err != nil {
		t.Fatalf("create duplicate: %v", err)
	}
	if err := st.MergeContacts(ctx, tenantID, primary.ID, duplicate.ID, "actor"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, err := st.GetContact(ctx, tenantID, duplicate.ID)
	if err != nil {
		t.Fatalf("get duplicate: %v", err)
	}
	if got.MergedInto == nil || *got.MergedInto != primary.ID {
		t.Fatalf("merged_into = %v, want %q", got.MergedInto, primary.ID)
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
