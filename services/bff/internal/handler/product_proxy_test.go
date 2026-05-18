package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/bff/internal/handler"
)

func TestProductProxyForwardsPathQueryAndContextHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/leads" {
			t.Fatalf("expected forwarded path /v1/leads, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "new" {
			t.Fatalf("expected query status=new, got %q", got)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-demo" {
			t.Fatalf("expected X-Tenant-ID propagated, got %q", got)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "tenant-demo" {
			t.Fatalf("expected X-Tenant-Id propagated, got %q", got)
		}
		if got := r.Header.Get("X-User-ID"); got != "user-demo" {
			t.Fatalf("expected X-User-ID propagated, got %q", got)
		}
		if got := r.Header.Get("X-User-Id"); got != "user-demo" {
			t.Fatalf("expected X-User-Id propagated, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"leads":[]}`))
	}))
	defer upstream.Close()

	proxy, err := handler.NewProductProxy(map[string]string{"lead_import": upstream.URL})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/leads?status=new", nil)
	req.Header.Set("X-Tenant-ID", "tenant-demo")
	req.Header.Set("X-User-ID", "user-demo")
	w := httptest.NewRecorder()

	proxy.ProxyTo("lead_import")(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEmptyLeadTimelineResponses(t *testing.T) {
	t.Run("activities", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/leads/lead-1/activities", nil)
		w := httptest.NewRecorder()
		handler.EmptyLeadActivities(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var body struct {
			Activities []any `json:"activities"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Activities) != 0 {
			t.Fatalf("expected no activities, got %d", len(body.Activities))
		}
	})

	t.Run("status history", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/leads/lead-1/status-history", nil)
		w := httptest.NewRecorder()
		handler.EmptyLeadStatusHistory(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var body struct {
			History []any `json:"history"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.History) != 0 {
			t.Fatalf("expected no history, got %d", len(body.History))
		}
	})
}
