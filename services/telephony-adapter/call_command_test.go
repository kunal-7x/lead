package telephony_adapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/handler"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

func TestCreateCallDemoUsesMockProviderAndStoresSession(t *testing.T) {
	st := store.NewFake()
	mock := adapter.NewMock()
	router := routing.New(st, []adapter.Telephony{mock})
	wh := webhook.New("secret", st, outbox.NewFakePublisher())
	h := handler.New(wh, router)

	body := map[string]any{
		"session_id":   "call-demo-001",
		"tenant_id":    "tenant-demo",
		"campaign_id":  "campaign-demo",
		"lead_id":      "lead-demo-001",
		"contact_id":   "contact-demo-001",
		"project_id":   "project-skyline",
		"from_number":  "+911400000000",
		"to_number":    "+919876543210",
		"callback_url": "http://voice-agent/ws/audio/call-demo-001",
		"region":       "IN",
		"max_duration": 300,
		"demo":         true,
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/calls", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["provider"] != "mock" {
		t.Fatalf("expected mock provider, got %#v", resp["provider"])
	}
	if resp["session_id"] != "call-demo-001" {
		t.Fatalf("expected stable session id, got %#v", resp["session_id"])
	}
	if mock.CallCount() != 1 {
		t.Fatalf("expected one mock call, got %d", mock.CallCount())
	}

	session, err := st.GetCallSession(context.Background(), "call-demo-001")
	if err != nil {
		t.Fatal(err)
	}
	if session.ProviderName != "mock" || session.ProviderCallID == "" {
		t.Fatalf("unexpected stored session: %#v", session)
	}
}

func TestCreateCallRequiresTenantAndPhone(t *testing.T) {
	st := store.NewFake()
	router := routing.New(st, []adapter.Telephony{adapter.NewMock()})
	wh := webhook.New("secret", st, outbox.NewFakePublisher())
	h := handler.New(wh, router)

	req := httptest.NewRequest(http.MethodPost, "/v1/calls", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
