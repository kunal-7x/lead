//go:build rls

package rls_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lead/services/tenant-auth/internal/store"
)

// TestRLS_TenantIsolation proves that a connection scoped to tenant A
// cannot read rows that belong to tenant B.
func TestRLS_TenantIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Bootstrap two tenants directly (bypasses RLS via superuser connection).
	pgA := store.NewPG(pool)
	tenantA, err := pgA.CreateTenant(ctx, "Tenant A", "us-east-1")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tenantB, err := pgA.CreateTenant(ctx, "Tenant B", "us-east-1")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	// Create a user in tenant A.
	userA, err := pgA.CreateUser(ctx, tenantA.ID, "a@example.com", "User A", "$argon2id$v=19$m=65536,t=1,p=2$abc$def")
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}

	// Create a user in tenant B.
	_, err = pgA.CreateUser(ctx, tenantB.ID, "b@example.com", "User B", "$argon2id$v=19$m=65536,t=1,p=2$abc$def")
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}

	const rlsRole = "tenant_auth_rls_test"
	_, err = pool.Exec(ctx, fmt.Sprintf(`
DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN
		CREATE ROLE %s;
	END IF;
END
$$;
GRANT SELECT ON tenant_users TO %s;`, rlsRole, rlsRole, rlsRole))
	if err != nil {
		t.Fatalf("create restricted test role: %v", err)
	}
	_, err = pool.Exec(ctx, fmt.Sprintf("GRANT %s TO CURRENT_USER", rlsRole))
	if err != nil {
		t.Skipf("database role cannot grant restricted RLS role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("REVOKE SELECT ON tenant_users FROM %s", rlsRole))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("REVOKE %s FROM CURRENT_USER", rlsRole))
	})

	// Query as a restricted role scoped to tenant B. The postgres superuser
	// bypasses RLS, so the assertion must run after SET ROLE.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL ROLE %s", rlsRole))
	if err != nil {
		t.Fatalf("set restricted role: %v", err)
	}
	_, err = tx.Exec(ctx, fmt.Sprintf(
		"SET LOCAL app.current_tenant_id = '%s'; SET LOCAL app.current_user_id = '%s'",
		tenantB.ID, "00000000-0000-0000-0000-000000000000"))
	if err != nil {
		t.Fatalf("set rls vars: %v", err)
	}

	// Try to read tenant A's user from a tenant B context.
	var count int
	err = tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM tenant_users WHERE id = $1", userA.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if count != 0 {
		t.Errorf("RLS FAILED: tenant B can read tenant A's user (count=%d)", count)
	} else {
		t.Logf("RLS PASS: tenant B cannot read tenant A's user")
	}
}
