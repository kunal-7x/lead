//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lead/services/consent-compliance/internal/handler"
	"github.com/lead/services/consent-compliance/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := store.NewFake()
	h := handler.New(s)
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h.Mount(r)
	return httptest.NewServer(r)
}

func TestIntegration_Healthz(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestIntegration_RecordAndExportConsent(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{
		"lead_id": "lead-int-1",
		"basis":   "explicit",
		"source":  "web_form",
		"notice_version": "v1.2",
	})
	resp, err := http.Post(srv.URL+"/v1/consent/record", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/v1/consent/trail/lead-int-1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	records, _ := out["records"].([]any)
	if len(records) == 0 {
		t.Error("expected at least one consent record in trail")
	}
}

func TestIntegration_CheckOutreachAllowed(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	ist, _ := time.LoadLocation("Asia/Kolkata")
	atTime := time.Date(2024, 1, 15, 10, 0, 0, 0, ist).Format(time.RFC3339)

	body, _ := json.Marshal(map[string]string{
		"lead_id": "lead-int-2",
		"channel": "phone",
		"at_time": atTime,
	})
	resp, err := http.Post(srv.URL+"/v1/consent/check-outreach", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["allowed"] != true {
		t.Errorf("expected allowed=true for 10:00 IST, got %v", out)
	}
}

func TestIntegration_SuppressionFlow(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Add suppression
	body, _ := json.Marshal(map[string]string{
		"phone":  "+919999999999",
		"reason": "DND",
	})
	resp, err := http.Post(srv.URL+"/v1/suppression", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	// Check outreach blocked
	ist, _ := time.LoadLocation("Asia/Kolkata")
	atTime := time.Date(2024, 1, 15, 10, 0, 0, 0, ist).Format(time.RFC3339)
	checkBody, _ := json.Marshal(map[string]string{
		"lead_id": "lead-int-3",
		"phone":   "+919999999999",
		"channel": "phone",
		"at_time": atTime,
	})
	resp, err = http.Post(srv.URL+"/v1/consent/check-outreach", "application/json", bytes.NewReader(checkBody))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["allowed"] != false {
		t.Error("expected suppressed phone to be blocked")
	}
	if out["reason"] != "suppression" {
		t.Errorf("expected reason 'suppression', got %v", out["reason"])
	}
}

func TestIntegration_PDFExport(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Record a consent first
	body, _ := json.Marshal(map[string]string{
		"lead_id": "lead-pdf-1",
		"basis":   "explicit",
		"source":  "web",
	})
	_, _ = http.Post(srv.URL+"/v1/consent/record", "application/json", bytes.NewReader(body))

	// Export as PDF
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/consent/trail/lead-pdf-1", nil)
	req.Header.Set("Accept", "application/pdf")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for PDF export, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("expected Content-Type application/pdf, got %q", resp.Header.Get("Content-Type"))
	}

	// Verify PDF starts with %PDF-
	buf := make([]byte, 5)
	_, _ = resp.Body.Read(buf)
	if string(buf) != "%PDF-" {
		t.Errorf("expected PDF to start with %%PDF-, got %q", string(buf))
	}
}

func TestIntegration_ErasureRequest(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{"lead_id": "lead-erase-1"})
	resp, err := http.Post(srv.URL+"/v1/consent/erasure", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["workflow_id"] == "" {
		t.Error("expected non-empty workflow_id in erasure response")
	}
}
