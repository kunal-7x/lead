package telephony_adapter_test

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/webhook"
)

// TestWebhook_RecordingEvent_PublishesReadyAndStoresRow verifies the C7
// recording handler: a verified "recording" webhook → CallRecording row
// inserted + call.recording.ready NATS event emitted with the provider URL.
func TestWebhook_RecordingEvent_PublishesReadyAndStoresRow(t *testing.T) {
	h, s, pub := newWebhookHandler(testAuthToken)
	ctx := context.Background()

	form := url.Values{}
	form.Set("RecordUrl", "https://recordings.plivo.com/rec-42.mp3")
	form.Set("RecordingID", "rec-42")
	form.Set("CallUUID", "call-42")
	form.Set("RecordingDuration", "27")
	form.Set("RecordingFormat", "mp3")
	body := []byte(form.Encode())

	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	if err := h.HandleEvent(ctx, "recording", testURL, testNonce, sig, "evt-rec-42", body); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if s.RecordingCount() != 1 {
		t.Fatalf("expected 1 stored recording, got %d", s.RecordingCount())
	}
	rec, err := s.GetRecording(ctx, "call-42")
	if err != nil {
		t.Fatalf("GetRecording: %v", err)
	}
	if rec.StorageKey != "https://recordings.plivo.com/rec-42.mp3" {
		t.Errorf("unexpected storage key %q", rec.StorageKey)
	}
	if rec.ID != "rec-42" {
		t.Errorf("unexpected recording id %q", rec.ID)
	}

	msgs := pub.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 published events (provider.event + recording.ready), got %d", len(msgs))
	}
	var foundReady bool
	for _, m := range msgs {
		if m.Subject != webhook.NATSSubjectRecordingReady {
			continue
		}
		foundReady = true
		var payload map[string]string
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			t.Fatalf("decode ready event: %v", err)
		}
		if payload["recording_url"] != "https://recordings.plivo.com/rec-42.mp3" {
			t.Errorf("ready event missing recording_url: %v", payload)
		}
		if payload["provider_call_id"] != "call-42" {
			t.Errorf("ready event missing provider_call_id: %v", payload)
		}
	}
	if !foundReady {
		t.Errorf("expected %q subject in published events", webhook.NATSSubjectRecordingReady)
	}
}

// TestWebhook_RecordingEvent_JSONPayload covers the JSON variant Plivo
// supports when the webhook is configured with Content-Type: application/json.
func TestWebhook_RecordingEvent_JSONPayload(t *testing.T) {
	h, s, pub := newWebhookHandler(testAuthToken)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]string{
		"RecordUrl":         "https://recordings.plivo.com/rec-99.mp3",
		"RecordingID":       "rec-99",
		"CallUUID":          "call-99",
		"RecordingDuration": "12",
		"RecordingFormat":   "mp3",
	})

	sig := webhook.ComputeSignature(testAuthToken, testURL, testNonce)
	if err := h.HandleEvent(ctx, "recording", testURL, testNonce, sig, "evt-rec-99", payload); err != nil {
		t.Fatalf("HandleEvent (JSON): %v", err)
	}
	if s.RecordingCount() != 1 {
		t.Fatalf("expected 1 stored recording (JSON), got %d", s.RecordingCount())
	}
	if pub.Count() != 2 {
		t.Fatalf("expected 2 events (JSON), got %d", pub.Count())
	}
}
