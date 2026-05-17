package telephony_adapter_test

import (
	"context"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

// TestIdempotency_SameEventIDTwice verifies that delivering the same event_id
// twice produces exactly one stored row and one NATS publish.
func TestIdempotency_SameEventIDTwice(t *testing.T) {
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	h := webhook.New(testAuthToken, s, pub)
	ctx := context.Background()

	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	eventID := "idp-evt-001"
	payload := []byte(`{"call_uuid":"abc"}`)

	if err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, eventID, payload); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, eventID, payload); err != nil {
		t.Fatalf("second delivery: %v", err)
	}

	if got := pub.Count(); got != 1 {
		t.Fatalf("expected 1 published event, got %d", got)
	}
	if got := s.WebhookEventCount(); got != 1 {
		t.Fatalf("expected 1 stored webhook event, got %d", got)
	}
	if got := s.IdempotencyKeyCount(); got != 1 {
		t.Fatalf("expected 1 idempotency key, got %d", got)
	}
}

// TestIdempotency_DifferentEventIDs verifies that two events with different IDs
// are each stored and published independently.
func TestIdempotency_DifferentEventIDs(t *testing.T) {
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	h := webhook.New(testAuthToken, s, pub)
	ctx := context.Background()

	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)

	if err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, "idp-evt-002", []byte(`{"call_uuid":"x"}`)); err != nil {
		t.Fatalf("event 1: %v", err)
	}
	if err := h.HandleEvent(ctx, "hangup", testURL, testNonce, sig, "idp-evt-003", []byte(`{"call_uuid":"y"}`)); err != nil {
		t.Fatalf("event 2: %v", err)
	}

	if got := pub.Count(); got != 2 {
		t.Fatalf("expected 2 published events, got %d", got)
	}
	if got := s.IdempotencyKeyCount(); got != 2 {
		t.Fatalf("expected 2 idempotency keys, got %d", got)
	}
}

// TestIdempotency_ReplayAfterInvalidFirstAttempt verifies that a replay of an event
// that previously failed signature check (and was therefore not stored) is processed
// normally on a valid subsequent attempt.
func TestIdempotency_ReplayAfterInvalidFirstAttempt(t *testing.T) {
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	h := webhook.New(testAuthToken, s, pub)
	ctx := context.Background()

	eventID := "idp-evt-004"
	payload := []byte(`{"call_uuid":"z"}`)

	// First attempt: invalid signature — rejected, not stored.
	_ = h.HandleEvent(ctx, "answer", testURL, testNonce, "bad-sig", eventID, payload)
	if pub.Count() != 0 {
		t.Fatal("expected 0 published after invalid sig")
	}

	// Second attempt: valid signature — should be accepted.
	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	if err := h.HandleEvent(ctx, "answer", testURL, testNonce, sig, eventID, payload); err != nil {
		t.Fatalf("valid retry: %v", err)
	}
	if pub.Count() != 1 {
		t.Fatalf("expected 1 published after valid retry, got %d", pub.Count())
	}
}
