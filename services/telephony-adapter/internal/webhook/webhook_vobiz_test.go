package webhook_test

import (
	"context"
	"os"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

const (
	testVobizSecret = "my-vobiz-webhook-secret"
)

func newVobizHandler() (*webhook.Handler, *store.Fake, *outbox.FakePublisher) {
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	h := webhook.New("plivo-token-unused", s, pub)
	return h, s, pub
}

// TestVerifyVobizSignature_Valid confirms a correctly signed body passes.
func TestVerifyVobizSignature_Valid(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")

	h, _, _ := newVobizHandler()
	body := []byte(`CallUUID=abc-123&CallStatus=ringing`)
	sig := webhook.ComputeVobizSignature(testVobizSecret, body)

	if !h.VerifyVobizSignature(body, sig) {
		t.Fatal("expected valid signature to pass VerifyVobizSignature")
	}
}

// TestVerifyVobizSignature_TamperedBody confirms a modified body is rejected.
func TestVerifyVobizSignature_TamperedBody(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")

	h, _, _ := newVobizHandler()
	originalBody := []byte(`CallUUID=abc-123&CallStatus=ringing`)
	tamperedBody := []byte(`CallUUID=abc-123&CallStatus=answered`) // changed
	sig := webhook.ComputeVobizSignature(testVobizSecret, originalBody)

	if h.VerifyVobizSignature(tamperedBody, sig) {
		t.Fatal("expected tampered body to fail VerifyVobizSignature")
	}
}

// TestVerifyVobizSignature_WrongSecret confirms a different secret is rejected.
func TestVerifyVobizSignature_WrongSecret(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")

	h, _, _ := newVobizHandler()
	body := []byte(`CallUUID=abc-123&CallStatus=ringing`)
	sig := webhook.ComputeVobizSignature("wrong-secret", body)

	if h.VerifyVobizSignature(body, sig) {
		t.Fatal("expected wrong secret to fail VerifyVobizSignature")
	}
}

// TestVerifyVobizSignature_MissingSig confirms an empty signature fails.
func TestVerifyVobizSignature_MissingSig(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")

	h, _, _ := newVobizHandler()
	body := []byte(`CallUUID=abc-123`)

	if h.VerifyVobizSignature(body, "") {
		t.Fatal("expected empty signature to fail VerifyVobizSignature")
	}
}

// TestHandleVobizEvent_StrictMode_InvalidSig confirms strict mode returns ErrVobizBadSignature.
func TestHandleVobizEvent_StrictMode_InvalidSig(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "strict")
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	h, _, pub := newVobizHandler()
	body := []byte(`CallUUID=call-strict-001&CallStatus=ringing`)

	err := h.HandleVobizEvent(context.Background(), "status", "call-strict-001", body, "badsig", "evt-strict-1",
		map[string]string{"CallUUID": "call-strict-001", "CallStatus": "ringing"})
	if err == nil {
		t.Fatal("expected error in strict mode with bad signature")
	}
	if err != webhook.ErrVobizBadSignature {
		t.Fatalf("expected ErrVobizBadSignature, got: %v", err)
	}
	if pub.Count() != 0 {
		t.Fatalf("expected 0 events published in strict mode rejection, got %d", pub.Count())
	}
}

// TestHandleVobizEvent_WarnMode_InvalidSig confirms default mode processes event despite bad sig.
func TestHandleVobizEvent_WarnMode_InvalidSig(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "true") // default warn-only mode
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	h, _, pub := newVobizHandler()
	body := []byte(`CallUUID=call-warn-001&CallStatus=ringing`)

	err := h.HandleVobizEvent(context.Background(), "status", "call-warn-001", body, "badsig", "evt-warn-1",
		map[string]string{"CallUUID": "call-warn-001", "CallStatus": "ringing"})
	if err != nil {
		t.Fatalf("expected no error in warn mode despite bad sig, got: %v", err)
	}
	// Should have published: call.provider.event + call.ringing
	if pub.Count() < 1 {
		t.Fatalf("expected events to be published in warn mode, got %d", pub.Count())
	}
}

// TestHandleVobizEvent_ValidSig_Published confirms a valid signature event is fully processed.
func TestHandleVobizEvent_ValidSig_Published(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "strict")
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	h, s, pub := newVobizHandler()
	body := []byte(`CallUUID=call-valid-001&CallStatus=ringing`)
	sig := webhook.ComputeVobizSignature(testVobizSecret, body)

	err := h.HandleVobizEvent(context.Background(), "status", "call-valid-001", body, sig, "evt-valid-1",
		map[string]string{"CallUUID": "call-valid-001", "CallStatus": "ringing"})
	if err != nil {
		t.Fatalf("unexpected error with valid sig: %v", err)
	}
	// call.provider.event + call.ringing
	if pub.Count() < 2 {
		t.Fatalf("expected at least 2 published events, got %d", pub.Count())
	}
	if s.WebhookEventCount() != 1 {
		t.Fatalf("expected 1 stored webhook event, got %d", s.WebhookEventCount())
	}
}

// TestHandleVobizEvent_Idempotency confirms replays are deduplicated.
func TestHandleVobizEvent_Idempotency(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_SECRET", testVobizSecret)
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "strict")
	defer os.Unsetenv("VOBIZ_WEBHOOK_SECRET")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	h, _, pub := newVobizHandler()
	body := []byte(`CallUUID=call-idem-001&CallStatus=answered`)
	sig := webhook.ComputeVobizSignature(testVobizSecret, body)
	params := map[string]string{"CallUUID": "call-idem-001", "CallStatus": "answered"}

	// First delivery.
	if err := h.HandleVobizEvent(context.Background(), "status", "call-idem-001", body, sig, "evt-idem-1", params); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	countAfterFirst := pub.Count()

	// Replay: same event_id.
	if err := h.HandleVobizEvent(context.Background(), "status", "call-idem-001", body, sig, "evt-idem-1", params); err != nil {
		t.Fatalf("replay delivery: %v", err)
	}
	if pub.Count() != countAfterFirst {
		t.Fatalf("replay should not publish additional events; count before=%d after=%d", countAfterFirst, pub.Count())
	}
}
