package service_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/lead-import/internal/service"
	"github.com/lead/services/lead-import/internal/store"
)

func newRouter(t *testing.T) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	svc := service.New(store.NewFake())
	svc.Mount(r)
	return r
}

func postJSON(t *testing.T, r http.Handler, path, tenantID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIngestApiLead(t *testing.T) {
	r := newRouter(t)
	w := postJSON(t, r, "/v1/leads", "tenant-1", map[string]string{
		"phone": "+919876543210",
		"name":  "Alice",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIngestApiLeadDedupe(t *testing.T) {
	r := newRouter(t)
	body := map[string]string{"phone": "+919876543210", "name": "Alice"}
	w1 := postJSON(t, r, "/v1/leads", "tenant-1", body)
	w2 := postJSON(t, r, "/v1/leads", "tenant-1", body)
	if w1.Code != http.StatusCreated || w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 twice, got %d and %d", w1.Code, w2.Code)
	}
	var r1, r2 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &r1)
	_ = json.Unmarshal(w2.Body.Bytes(), &r2)
	c1 := r1["contact"].(map[string]any)["id"].(string)
	c2 := r2["contact"].(map[string]any)["id"].(string)
	if c1 != c2 {
		t.Errorf("same phone should yield same contact: %q != %q", c1, c2)
	}
}

func TestIngestApiLeadInvalidPhone(t *testing.T) {
	r := newRouter(t)
	w := postJSON(t, r, "/v1/leads", "tenant-1", map[string]string{
		"phone": "notaphone",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid phone, got %d", w.Code)
	}
}

func TestCreateImportJobInlineRows(t *testing.T) {
	r := newRouter(t)
	rows := []map[string]string{
		{"phone": "+919876543210", "name": "Alice"},
		{"phone": "+919876543211", "name": "Bob"},
		{"phone": "notaphone", "name": "Bad"},
	}
	w := postJSON(t, r, "/v1/import/jobs", "tenant-1", map[string]any{
		"rows":    rows,
		"mapping": map[string]string{},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	job := resp["job"].(map[string]any)
	if job["imported_rows"].(float64) != 2 {
		t.Errorf("expected 2 imported rows, got %v", job["imported_rows"])
	}
	if job["error_rows"].(float64) != 1 {
		t.Errorf("expected 1 error row, got %v", job["error_rows"])
	}
}

func TestTenantIsolation(t *testing.T) {
	r := newRouter(t)
	body := map[string]string{"phone": "+919876543210", "name": "Alice"}
	postJSON(t, r, "/v1/leads", "tenant-A", body)

	// Listing leads for tenant-B should return empty.
	req := httptest.NewRequest(http.MethodGet, "/v1/leads", nil)
	req.Header.Set("X-Tenant-ID", "tenant-B")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	leads := resp["leads"]
	if leads != nil {
		if arr, ok := leads.([]any); ok && len(arr) > 0 {
			t.Errorf("tenant-B should see no leads, got %d", len(arr))
		}
	}
}
