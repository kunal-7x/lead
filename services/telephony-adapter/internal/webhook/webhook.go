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
	"net/url"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/store"
)

const (
	// NATSSubjectProviderEvent is the NATS subject for normalized provider events.
	NATSSubjectProviderEvent = "call.provider.event"
	// NATSSubjectRecordingReady is emitted when a provider posts a recording URL.
	// Consumed by freeswitch-bridge's recording uploader (C8) to pull & re-store
	// the audio in DO Spaces and archive a copy in B2 with Object Lock.
	NATSSubjectRecordingReady = "call.recording.ready"

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
		"event_id":   eventID,
		"event_type": eventType,
		"provider":   "plivo",
	})
	if err := h.publisher.Publish(ctx, NATSSubjectProviderEvent, pubData); err != nil {
		return fmt.Errorf("webhook: publish: %w", err)
	}

	// Recording-specific handling: parse the provider recording URL, persist a
	// CallRecording row pointing at the raw provider URL, and emit
	// call.recording.ready so the uploader (C8) can pull and re-store it.
	if eventType == "recording" {
		if err := h.handleRecording(ctx, eventID, payload); err != nil {
			return fmt.Errorf("webhook: handle recording: %w", err)
		}
	}

	if err := h.store.SetIdempotencyKey(ctx, eventID, []byte("published")); err != nil {
		return fmt.Errorf("webhook: set idempotency key: %w", err)
	}

	return nil
}

// PlivoRecordingPayload mirrors Plivo's recording webhook fields (form OR JSON).
type PlivoRecordingPayload struct {
	RecordURL         string `json:"RecordUrl"`
	RecordingID       string `json:"RecordingID"`
	CallUUID          string `json:"CallUUID"`
	RecordingDuration string `json:"RecordingDuration"`
	RecordingFormat   string `json:"RecordingFormat"`
}

// parseRecordingPayload accepts either form-urlencoded or JSON bodies.
func parseRecordingPayload(body []byte) (PlivoRecordingPayload, error) {
	var out PlivoRecordingPayload
	// Try JSON first
	if json.Unmarshal(body, &out) == nil && out.RecordURL != "" {
		return out, nil
	}
	// Fall back to form encoding
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return out, fmt.Errorf("parse recording payload: %w", err)
	}
	out.RecordURL = vals.Get("RecordUrl")
	out.RecordingID = vals.Get("RecordingID")
	out.CallUUID = vals.Get("CallUUID")
	out.RecordingDuration = vals.Get("RecordingDuration")
	out.RecordingFormat = vals.Get("RecordingFormat")
	if out.RecordURL == "" {
		return out, fmt.Errorf("parse recording payload: no RecordUrl")
	}
	return out, nil
}

func (h *Handler) handleRecording(ctx context.Context, eventID string, payload []byte) error {
	p, err := parseRecordingPayload(payload)
	if err != nil {
		return err
	}

	rec := &model.CallRecording{
		ID:             p.RecordingID,
		ProviderCallID: p.CallUUID,
		StorageKey:     p.RecordURL, // raw provider URL until C8's uploader rewrites it
		Encrypted:      false,
		CreatedAt:      time.Now(),
	}
	if rec.ID == "" {
		rec.ID = "rec-" + p.CallUUID
	}
	if err := h.store.StoreRecording(ctx, rec); err != nil {
		return fmt.Errorf("store recording: %w", err)
	}

	readyEvt, _ := json.Marshal(map[string]string{
		"event_id":         eventID,
		"provider":         "plivo",
		"provider_call_id": p.CallUUID,
		"recording_id":     rec.ID,
		"recording_url":    p.RecordURL,
		"format":           p.RecordingFormat,
	})
	if err := h.publisher.Publish(ctx, NATSSubjectRecordingReady, readyEvt); err != nil {
		return fmt.Errorf("publish recording.ready: %w", err)
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
