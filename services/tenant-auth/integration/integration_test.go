//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lead/services/tenant-auth/internal/ratelimit"
	"github.com/lead/services/tenant-auth/internal/service"
	"github.com/lead/services/tenant-auth/internal/store"
	"github.com/lead/services/tenant-auth/internal/vault"
)

func newIntegrationService(t *testing.T) (*service.Service, *httptest.Server) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(pool.Close)

	pg := store.NewPG(pool)
	svc := service.New(pg, vault.NewStatic(""), &ratelimit.NoOp{})
	mux := http.NewServeMux()
	svc.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return svc, srv
}

func post(t *testing.T, srv *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return v
}

const base = "/evs.v1.TenantAuthService/"

func TestIntegration_TenantCRUD(t *testing.T) {
	_, srv := newIntegrationService(t)

	resp := post(t, srv, base+"CreateTenant", map[string]any{"name": "Integration Tenant", "region": "us-west-2"})
	if resp["tenant"] == nil {
		t.Fatalf("expected tenant in response: %v", resp)
	}
	tid := resp["tenant"].(map[string]any)["id"].(string)

	get := post(t, srv, base+"GetTenant", map[string]any{"tenant_id": tid})
	if get["tenant"] == nil {
		t.Fatalf("expected tenant: %v", get)
	}

	upd := post(t, srv, base+"UpdateTenant", map[string]any{"tenant_id": tid, "name": "Updated Tenant"})
	if upd["tenant"].(map[string]any)["name"] != "Updated Tenant" {
		t.Errorf("expected updated name: %v", upd)
	}

	susp := post(t, srv, base+"SuspendTenant", map[string]any{"tenant_id": tid})
	if susp["ok"] != true {
		t.Errorf("expected ok: %v", susp)
	}
}

func TestIntegration_LoginJWTRoundTrip(t *testing.T) {
	_, srv := newIntegrationService(t)

	// Create tenant + user
	tr := post(t, srv, base+"CreateTenant", map[string]any{"name": "JWT Tenant"})
	tid := tr["tenant"].(map[string]any)["id"].(string)

	post(t, srv, base+"CreateUser", map[string]any{
		"tenant_id": tid, "email": "jwt@example.com", "password": "Passw0rd!", "display_name": "JWT User",
	})

	// Login
	lr := post(t, srv, base+"Login", map[string]any{
		"tenant_id": tid, "email": "jwt@example.com", "password": "Passw0rd!",
	})
	if lr["access_token"] == nil {
		t.Fatalf("no access_token: %v", lr)
	}

	// Refresh
	rr := post(t, srv, base+"RefreshToken", map[string]any{
		"tenant_id":     tid,
		"refresh_token": lr["refresh_token"].(string),
	})
	if rr["access_token"] == nil {
		t.Fatalf("no access_token after refresh: %v", rr)
	}
}

func TestIntegration_AuditLog(t *testing.T) {
	_, srv := newIntegrationService(t)
	tr := post(t, srv, base+"CreateTenant", map[string]any{"name": "Audit Tenant"})
	tid := tr["tenant"].(map[string]any)["id"].(string)

	post(t, srv, base+"WriteAuditLog", map[string]any{
		"tenant_id": tid, "actor_id": "sys", "action": "test.event", "resource": "lead",
	})

	qr := post(t, srv, base+"QueryAuditLogs", map[string]any{"tenant_id": tid, "limit": 10})
	if qr["logs"] == nil {
		t.Fatalf("expected logs: %v", qr)
	}
}
