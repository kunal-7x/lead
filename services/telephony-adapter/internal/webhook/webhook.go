// Package webhook handles inbound Plivo webhooks with signature verification and idempotency.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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

	// NATS lifecycle subjects emitted by Vobiz status/hangup webhooks.
	NATSSubjectCallRinging   = "call.ringing"
	NATSSubjectCallAnswered  = "call.answered"
	NATSSubjectCallCompleted = "call.completed"
	NATSSubjectCallFailed    = "call.failed"
	NATSSubjectCallInitiated = "call.initiated"

	headerSignature = "X-Plivo-Signature-V3"
	headerNonce     = "X-Plivo-Signature-Nonce"
	headerEventID   = "X-Plivo-Event-ID"

	// headerVobizSignature is the HMAC-SHA256 hex signature Vobiz attaches to webhook POSTs.
	// Computed as: hex(HMAC-SHA256(raw_request_body, VOBIZ_WEBHOOK_SECRET))
	headerVobizSignature = "X-Signature"
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

// VerifyVobizSignature checks the Vobiz HMAC-SHA256 webhook signature.
// The signature is hex(HMAC-SHA256(rawBody, VOBIZ_WEBHOOK_SECRET)).
// signatureHex should be the value of the X-Signature header.
func (h *Handler) VerifyVobizSignature(rawBody []byte, signatureHex string) bool {
	secret := os.Getenv("VOBIZ_WEBHOOK_SECRET")
	if secret == "" {
		// No secret configured; cannot verify — treat as unverifiable (caller decides).
		return false
	}
	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := mac.Sum(nil)
	return hmac.Equal(expected, sigBytes)
}

// ComputeVobizSignature produces a valid Vobiz X-Signature for testing.
func ComputeVobizSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// vobizVerifyMode returns the value of VOBIZ_WEBHOOK_VERIFY env (default "true").
// "true"   → warn-only on bad sig, never reject (live-call safety).
// "strict" → reject with 401 on bad sig.
func vobizVerifyMode() string {
	mode := os.Getenv("VOBIZ_WEBHOOK_VERIFY")
	if mode == "" {
		return "true"
	}
	return mode
}

// HandleVobizEvent processes one inbound Vobiz status/recording/hangup event.
// It enforces idempotency using (provider='vobiz', providerCallID, eventType).
//
// Signature enforcement:
//   - mode "true"   (default): invalid/missing sig → log warning, still process.
//   - mode "strict"           : invalid/missing sig → return ErrVobizBadSignature (caller 401s).
//   - Answer URL requests are always processed regardless (Vobiz may not sign them).
func (h *Handler) HandleVobizEvent(
	ctx context.Context,
	eventType string,         // "status", "recording", "hangup", etc.
	providerCallID string,    // CallUUID from Vobiz payload
	rawBody []byte,
	signatureHex string,      // value of X-Signature header (may be empty)
	eventID string,           // synthetic idempotency key
	payload map[string]string, // pre-parsed fields from body
) error {
	// Signature check.
	sigOK := h.VerifyVobizSignature(rawBody, signatureHex)
	if !sigOK {
		mode := vobizVerifyMode()
		if mode == "strict" {
			return ErrVobizBadSignature
		}
		// warn-only: log but continue processing to avoid dropping live call events.
		log.Printf("webhook: vobiz: signature missing or invalid (mode=%s, event_type=%s, call_uuid=%s) — processing anyway",
			mode, eventType, providerCallID)
	}

	// Idempotency: key = "vobiz:<eventType>:<providerCallID>"
	idempKey := fmt.Sprintf("vobiz:%s:%s:%s", eventType, providerCallID, eventID)
	_, exists, err := h.store.CheckIdempotencyKey(ctx, idempKey)
	if err != nil {
		return fmt.Errorf("webhook: vobiz: idempotency check: %w", err)
	}
	if exists {
		return nil // replay — already processed
	}

	// Store raw webhook event.
	evt := &model.WebhookEvent{
		ID:           idempKey,
		ProviderName: "vobiz",
		EventType:    eventType,
		RawPayload:   rawBody,
		ReceivedAt:   time.Now(),
		Published:    false,
	}
	if err := h.store.StoreWebhookEvent(ctx, evt); err != nil {
		return fmt.Errorf("webhook: vobiz: store event: %w", err)
	}

	// Store provider event row.
	provEvt := &model.CallProviderEvent{
		ID:             idempKey,
		ProviderCallID: providerCallID,
		ProviderName:   "vobiz",
		EventType:      eventType,
		RawPayload:     rawBody,
		ReceivedAt:     time.Now(),
	}
	if err := h.store.StoreProviderEvent(ctx, provEvt); err != nil {
		return fmt.Errorf("webhook: vobiz: store provider event: %w", err)
	}

	// Publish call.provider.event.
	provEvtData, _ := json.Marshal(map[string]string{
		"event_id":         idempKey,
		"event_type":       eventType,
		"provider":         "vobiz",
		"provider_call_id": providerCallID,
	})
	if err := h.publisher.Publish(ctx, NATSSubjectProviderEvent, provEvtData); err != nil {
		return fmt.Errorf("webhook: vobiz: publish provider event: %w", err)
	}

	// Publish lifecycle NATS subject based on Vobiz status value.
	lifecycleSubject := vobizLifecycleSubject(eventType, payload["CallStatus"])
	if lifecycleSubject != "" {
		lData, _ := json.Marshal(map[string]string{
			"event_id":         idempKey,
			"provider":         "vobiz",
			"provider_call_id": providerCallID,
			"status":           payload["CallStatus"],
		})
		if err := h.publisher.Publish(ctx, lifecycleSubject, lData); err != nil {
			return fmt.Errorf("webhook: vobiz: publish lifecycle event: %w", err)
		}
	}

	// Recording-specific handling.
	if eventType == "recording" {
		if recURL, ok := payload["RecordUrl"]; ok && recURL != "" {
			if err := h.handleVobizRecording(ctx, idempKey, providerCallID, payload, rawBody); err != nil {
				return fmt.Errorf("webhook: vobiz: handle recording: %w", err)
			}
		}
	}

	// Mark idempotency key as processed.
	if err := h.store.SetIdempotencyKey(ctx, idempKey, []byte("published")); err != nil {
		return fmt.Errorf("webhook: vobiz: set idempotency key: %w", err)
	}

	return nil
}

// vobizLifecycleSubject maps Vobiz status strings to NATS lifecycle subjects.
// For hangup/status events, we interpret CallStatus; for recording, no lifecycle.
// Vobiz status values (Plivo-family): initiated, ringing, answered, completed, failed, busy, no-answer.
func vobizLifecycleSubject(eventType, callStatus string) string {
	switch eventType {
	case "hangup", "status":
		switch callStatus {
		case "initiated":
			return NATSSubjectCallInitiated
		case "ringing":
			return NATSSubjectCallRinging
		case "answered", "in-progress":
			return NATSSubjectCallAnswered
		case "completed":
			return NATSSubjectCallCompleted
		case "failed", "busy", "no-answer", "canceled":
			return NATSSubjectCallFailed
		}
	}
	return ""
}

// handleVobizRecording persists a CallRecording row and emits call.recording.ready.
func (h *Handler) handleVobizRecording(ctx context.Context, eventID, providerCallID string, payload map[string]string, rawBody []byte) error {
	recURL := payload["RecordUrl"]
	recID := payload["RecordingID"]
	if recID == "" {
		recID = "vobizrec-" + providerCallID
	}

	rec := &model.CallRecording{
		ID:             recID,
		ProviderCallID: providerCallID,
		StorageKey:     recURL,
		Encrypted:      false,
		CreatedAt:      time.Now(),
	}
	if err := h.store.StoreRecording(ctx, rec); err != nil {
		return fmt.Errorf("store vobiz recording: %w", err)
	}

	readyEvt, _ := json.Marshal(map[string]string{
		"event_id":         eventID,
		"provider":         "vobiz",
		"provider_call_id": providerCallID,
		"recording_id":     recID,
		"recording_url":    recURL,
		"format":           payload["RecordingFormat"],
	})
	if err := h.publisher.Publish(ctx, NATSSubjectRecordingReady, readyEvt); err != nil {
		return fmt.Errorf("publish vobiz recording.ready: %w", err)
	}
	return nil
}

// ErrVobizBadSignature is returned by HandleVobizEvent in strict mode when the
// X-Signature header is missing or does not match.
var ErrVobizBadSignature = fmt.Errorf("webhook: vobiz: invalid signature")

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
