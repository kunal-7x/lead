//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/internal-admin-api/internal/model"
)

func TestPostgresStoreInternalAdminRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenants, err := st.ListTenants(ctx)
	if err != nil {
		t.Fatalf("list tenants: %v", err)
	}
	if len(tenants) == 0 {
		t.Fatal("seed tenants empty")
	}
	entry := model.AuditEntry{ActorID: testID("actor"), ActorRole: model.RoleOpsAdmin, Action: "tenant.update", TargetType: "tenant", TargetID: tenants[0].ID, TenantID: tenants[0].ID}
	if err := st.AppendAudit(ctx, entry); err != nil {
		t.Fatalf("append audit: %v", err)
	}
	audit, err := st.ListAudit(ctx, model.AuditFilter{TenantID: tenants[0].ID, Action: "tenant.update"})
	if err != nil || len(audit) != 1 {
		t.Fatalf("audit len=%d err=%v", len(audit), err)
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
