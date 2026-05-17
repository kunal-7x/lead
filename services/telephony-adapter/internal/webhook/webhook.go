// Package webhook handles inbound Plivo webhooks with signature verification and idempotency.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/store"
)

const (
	// NATSSubjectProviderEvent is the NATS subject for normalized provider events.
	NATSSubjectProviderEvent = "call.provider.event"

	headerSignature = "X-Plivo-Signature-V3"
	headerNonce     = "X-Plivo-Signature-Nonce"
	headerEventID   = "X-Plivo-Event-ID"
)

// Handler processes Plivo webhook events.
type Handler struct {
	authToken string
	store     store.Store
	publisher outbox.Publisher
}

func New(authToken string, s store.Store, pub outbox.Publisher) *Handler {
	return &Handler{authToken: authToken, store: s, publisher: pub}
}

// VerifySignature checks the Plivo HMAC-SHA256 webhook signature.
// Plivo v3: HMAC-SHA256(url + nonce, authToken), base64-encoded.
func (h *Handler) VerifySignature(url, nonce, signature string) bool {
	mac := hmac.New(sha256.New, []byte(h.authToken))
	mac.Write([]byte(url + nonce))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ComputeSignature produces a valid Plivo-style HMAC-SHA256 signature for testing.
func ComputeSignature(authToken, url, nonce string) string {
	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write([]byte(url + nonce))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// HandleEvent processes one inbound event from Plivo.
// It: verifies signature, checks idempotency, stores the event, and publishes to NATS outbox.
func (h *Handler) HandleEvent(ctx context.Context, eventType, url, nonce, signature, eventID string, payload []byte) error {
	if !h.VerifySignature(url, nonce, signature) {
		return fmt.Errorf("webhook: invalid signature")
	}

	// Idempotency check: if we've seen this event_id before, drop it.
	_, exists, err := h.store.CheckIdempotencyKey(ctx, eventID)
	if err != nil {
		return fmt.Errorf("webhook: idempotency check: %w", err)
	}
	if exists {
		return nil // replay — already processed
	}

	evt := &model.WebhookEvent{
		ID:           eventID,
		ProviderName: "plivo",
		EventType:    eventType,
		RawPayload:   payload,
		ReceivedAt:   time.Now(),
		Published:    false,
	}
	if err := h.store.StoreWebhookEvent(ctx, evt); err != nil {
		return fmt.Errorf("webhook: store event: %w", err)
	}

	pubData, _ := json.Marshal(map[string]string{
		"event_id":    eventID,
		"event_type":  eventType,
		"provider":    "plivo",
	})
	if err := h.publisher.Publish(ctx, NATSSubjectProviderEvent, pubData); err != nil {
		return fmt.Errorf("webhook: publish: %w", err)
	}

	if err := h.store.SetIdempotencyKey(ctx, eventID, []byte("published")); err != nil {
		return fmt.Errorf("webhook: set idempotency key: %w", err)
	}

	return nil
}

// ServeHTTP implements http.Handler so the handler can be mounted on a chi router.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	url := r.URL.String()
	nonce := r.Header.Get(headerNonce)
	signature := r.Header.Get(headerSignature)
	eventID := r.Header.Get(headerEventID)
	if eventID == "" {
		eventID = fmt.Sprintf("plivo-%d", time.Now().UnixNano())
	}
	eventType := r.PathValue("event")
	if eventType == "" {
		eventType = "unknown"
	}

	if err := h.HandleEvent(r.Context(), eventType, url, nonce, signature, eventID, body); err != nil {
		if err.Error() == "webhook: invalid signature" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
