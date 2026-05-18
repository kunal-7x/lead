package internaladminapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/internal-admin-api/internal/handler"
	"github.com/lead/services/internal-admin-api/internal/service"
	"github.com/lead/services/internal-admin-api/internal/store"
)

func TestClientOwnerCannotHitInternalAdminAPI(t *testing.T) {
	mux := http.NewServeMux()
	handler.New(service.New(store.NewFake())).Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/tenants", nil)
	req.Header.Set("X-Actor-ID", "client-1")
	req.Header.Set("X-Actor-Role", "client_owner")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
