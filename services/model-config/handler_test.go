package modelconfig_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/model-config/internal/handler"
	"github.com/lead/services/model-config/internal/service"
	"github.com/lead/services/model-config/internal/store"
)

func TestAdminModelsRequiresSuperAdmin(t *testing.T) {
	mux := http.NewServeMux()
	handler.New(service.New(store.NewFake())).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/models/current", nil)
	req.Header.Set("X-Admin-Role", "ops_admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
