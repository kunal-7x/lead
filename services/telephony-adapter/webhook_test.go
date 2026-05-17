package telephony_adapter_test

import (
	"context"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

const (
	testAuthToken = "test-auth-token-secret"
	testURL       = "https://example.com/wh/plivo/answer"
	testNonce     = "nonce-abc-123"
)

func newWebhookHandler(authToken string) (*webhook.Handler, *store.Fake, *outbox.FakePublisher) {
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	h := webhook.New(authToken, s, pub)
	return h, s, pub
}

// TestWebhook_InvalidSignature_Rejected verifies that a tampered signature is rejected.
func TestWebhook_InvalidSignature_Rejected(t *testing.T) {
	h, _, pub := newWebhookHandler(testAuthToken)
	ctx := context.Background()

	err := h.HandleEvent(ctx, "answer", testURL, testNonce, "invalid-sig", "evt-001", []byte(`{"call_uuid":"123"}`))
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
	if pub.Count() != 0 {
		t.Fatalf("expected 0 published events, got %d", pub.Count())
	}
}

// TestWebhook_ValidSignature_Accepted verifies that a correctly signed event is accepted and published.
func TestWebhook_ValidSignature_Accepted(t *testing.T) {
	h, s, pub := newWebhookHandler(testAuthToken)
	ctx := context.Background()

	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, "evt-002", []byte(`{"call_uuid":"123"}`))
	if err != nil {
		t.Fatalf("unexpected error for valid signature: %v", err)
	}
	if pub.Count() != 1 {
		t.Fatalf("expected 1 published event, got %d", pub.Count())
	}
	if s.WebhookEventCount() != 1 {
		t.Fatalf("expected 1 stored webhook event, got %d", s.WebhookEventCount())
	}
	if s.IdempotencyKeyCount() != 1 {
		t.Fatalf("expected 1 idempotency key, got %d", s.IdempotencyKeyCount())
	}
}

// TestWebhook_TamperedPayload_Rejected verifies that changing the nonce after signing is rejected.
func TestWebhook_TamperedPayload_Rejected(t *testing.T) {
	h, _, pub := newWebhookHandler(testAuthToken)
	ctx := context.Background()

	// Compute sig for original nonce, then send with different nonce.
	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	err := h.HandleEvent(ctx, "hangup", testURL, "tampered-nonce", sig, "evt-003", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for tampered nonce, got nil")
	}
	if pub.Count() != 0 {
		t.Fatalf("expected 0 published events after tampered payload, got %d", pub.Count())
	}
}

// TestWebhook_ReplayAttack_Dropped verifies that sending the same event_id twice
// produces exactly one downstream publish.
func TestWebhook_ReplayAttack_Dropped(t *testing.T) {
	h, s, pub := newWebhookHandler(testAuthToken)
	ctx := context.Background()

	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	eventID := "evt-replay-001"

	// First delivery.
	if err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, eventID, []byte(`{"call_uuid":"999"}`)); err != nil {
		t.Fatalf("first delivery: unexpected error: %v", err)
	}

	// Second delivery (replay).
	if err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, eventID, []byte(`{"call_uuid":"999"}`)); err != nil {
		t.Fatalf("replay delivery: unexpected error: %v", err)
	}

	if pub.Count() != 1 {
		t.Fatalf("replay: expected exactly 1 published event, got %d", pub.Count())
	}
	if s.IdempotencyKeyCount() != 1 {
		t.Fatalf("replay: expected 1 idempotency key, got %d", s.IdempotencyKeyCount())
	}
}
