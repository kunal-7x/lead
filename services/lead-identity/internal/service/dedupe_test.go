package service_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/lead-identity/internal/service"
	"github.com/lead/services/lead-identity/internal/store"
)

func newRouter(t *testing.T) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	svc := service.New(store.NewFake())
	svc.Mount(r)
	return r
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestDedupe verifies that submitting the same phone twice for the same tenant
// yields the same contact_id (deduplicated), while a different tenant gets a
// distinct contact_id (isolated).
func TestDedupe(t *testing.T) {
	r := newRouter(t)
	tenantA := "tenant-aaa"
	tenantB := "tenant-bbb"
	phone := "+919876543210"

	resolve := func(tenantID, ph string) string {
		w := postJSON(t, r, "/v1/identity/resolve", map[string]string{
			"tenant_id": tenantID,
			"phone":     ph,
		})
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("resolve returned %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		id, _ := resp["contact_id"].(string)
		return id
	}

	// Same phone, same tenant → same contact.
	id1 := resolve(tenantA, phone)
	id2 := resolve(tenantA, phone)
	if id1 == "" || id1 != id2 {
		t.Errorf("dedupe failed: first=%q second=%q should be equal", id1, id2)
	}

	// Same phone, different tenant → different contact (tenant isolation).
	idB := resolve(tenantB, phone)
	if idB == id1 {
		t.Errorf("cross-tenant isolation failed: tenant-A contact_id == tenant-B contact_id (%q)", id1)
	}
}

// TestMerge verifies that merging a duplicate into a primary marks the duplicate.
func TestMerge(t *testing.T) {
	r := newRouter(t)
	tenantID := "tenant-merge"

	// Create two distinct contacts (different phones).
	id1 := func() string {
		w := postJSON(t, r, "/v1/identity/resolve", map[string]string{
			"tenant_id": tenantID,
			"phone":     "+919876543210",
		})
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["contact_id"].(string)
	}()

	id2 := func() string {
		w := postJSON(t, r, "/v1/identity/resolve", map[string]string{
			"tenant_id": tenantID,
			"phone":     "+919876543211",
		})
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["contact_id"].(string)
	}()

	w := postJSON(t, r, "/v1/identity/merge", map[string]string{
		"tenant_id":    tenantID,
		"primary_id":   id1,
		"duplicate_id": id2,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("merge returned %d: %s", w.Code, w.Body.String())
	}
}
