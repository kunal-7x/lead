//go:build integration

package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/handler"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

const integTestAuthToken = "integ-auth-token"

func newTestServer() (*httptest.Server, *store.Fake, *outbox.FakePublisher) {
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	wh := webhook.New(integTestAuthToken, s, pub)

	adapters := []adapter.Telephony{
		adapter.NewMock(),
	}
	r := routing.New(s, adapters)
	h := handler.New(wh, r)
	return httptest.NewServer(h.Routes()), s, pub
}

func TestIntegration_Healthz(t *testing.T) {
	srv, _, _ := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestIntegration_Webhook_ValidSignature(t *testing.T) {
	srv, s, pub := newTestServer()
	defer srv.Close()

	url := srv.URL + "/wh/plivo/answer"
	nonce := "integ-nonce-1"
	sig := webhook.ComputeSignature(integTestAuthToken, "/wh/plivo/answer", nonce)

	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"call_uuid":"integ-001"}`))
	req.Header.Set("X-Plivo-Signature-V3", sig)
	req.Header.Set("X-Plivo-Signature-Nonce", nonce)
	req.Header.Set("X-Plivo-Event-ID", "integ-evt-001")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST webhook: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if pub.Count() != 1 {
		t.Fatalf("expected 1 published event, got %d", pub.Count())
	}
	if s.WebhookEventCount() != 1 {
		t.Fatalf("expected 1 stored event, got %d", s.WebhookEventCount())
	}
}

func TestIntegration_Webhook_InvalidSignature_Returns403(t *testing.T) {
	srv, _, pub := newTestServer()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/wh/plivo/answer", strings.NewReader(`{}`))
	req.Header.Set("X-Plivo-Signature-V3", "bad-signature")
	req.Header.Set("X-Plivo-Signature-Nonce", "nonce")
	req.Header.Set("X-Plivo-Event-ID", "integ-evt-002")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST webhook: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if pub.Count() != 0 {
		t.Fatalf("expected 0 published events, got %d", pub.Count())
	}
}

func TestIntegration_Webhook_ReplayDropped(t *testing.T) {
	srv, _, pub := newTestServer()
	defer srv.Close()

	url := srv.URL + "/wh/plivo/hangup"
	nonce := "integ-nonce-2"
	sig := webhook.ComputeSignature(integTestAuthToken, "/wh/plivo/hangup", nonce)

	sendEvent := func() int {
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"call_uuid":"replay-test"}`))
		req.Header.Set("X-Plivo-Signature-V3", sig)
		req.Header.Set("X-Plivo-Signature-Nonce", nonce)
		req.Header.Set("X-Plivo-Event-ID", "integ-evt-replay")
		resp, _ := http.DefaultClient.Do(req)
		return resp.StatusCode
	}

	if code := sendEvent(); code != http.StatusNoContent {
		t.Fatalf("first delivery: expected 204, got %d", code)
	}
	if code := sendEvent(); code != http.StatusNoContent {
		t.Fatalf("replay delivery: expected 204, got %d", code)
	}
	if pub.Count() != 1 {
		t.Fatalf("replay: expected 1 published event, got %d", pub.Count())
	}
}

func TestIntegration_ListProviders_ReturnsProvider(t *testing.T) {
	srv, _, _ := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/telephony/providers")
	if err != nil {
		t.Fatalf("GET /v1/telephony/providers: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
