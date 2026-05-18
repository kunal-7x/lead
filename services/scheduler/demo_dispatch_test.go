package scheduler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lead/services/scheduler/internal/handler"
	"github.com/lead/services/scheduler/internal/picker"
	"github.com/lead/services/scheduler/internal/store"
)

func TestDemoDispatchCallsTelephonyForPickedLeads(t *testing.T) {
	var received []map[string]any
	telephony := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/calls" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		received = append(received, body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":       body["session_id"],
			"provider":         "mock",
			"provider_call_id": "mock-" + body["lead_id"].(string),
			"status":           "queued",
		})
	}))
	defer telephony.Close()

	st := store.NewFake()
	h := handler.NewWithTelephony(picker.New(st), telephony.URL, telephony.Client())

	body := `{
		"tenant_id":"tenant-demo",
		"campaign_id":"campaign-demo",
		"worker_id":"worker-demo",
		"batch_size":2,
		"from_number":"+911400000000",
		"demo":true,
		"leads":[
			{"id":"lead-demo-001","to_number":"+919876543210","priority_score":80},
			{"id":"lead-demo-002","to_number":"+919988776655","priority_score":95}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/scheduler/demo-dispatch", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(received) != 2 {
		t.Fatalf("expected two telephony calls, got %d", len(received))
	}
	if received[0]["lead_id"] != "lead-demo-002" {
		t.Fatalf("expected highest priority lead first, got %#v", received[0]["lead_id"])
	}
	var resp struct {
		Attempted int `json:"attempted"`
		Calls     []struct {
			LeadID   string `json:"lead_id"`
			Provider string `json:"provider"`
			Status   string `json:"status"`
		} `json:"calls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Attempted != 2 || len(resp.Calls) != 2 {
		t.Fatalf("unexpected dispatch response: %#v", resp)
	}
	if resp.Calls[0].Provider != "mock" || resp.Calls[0].Status != "queued" {
		t.Fatalf("unexpected first call: %#v", resp.Calls[0])
	}
}
