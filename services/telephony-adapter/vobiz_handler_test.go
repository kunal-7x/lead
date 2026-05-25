package telephony_adapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/concurrency"
	"github.com/lead/services/telephony-adapter/internal/handler"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/outbox"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

// buildVobizHandler wires up a Handler with fakes suitable for Vobiz webhook tests.
func buildVobizHandler(t *testing.T) (http.Handler, *store.Fake, *outbox.FakePublisher) {
	t.Helper()
	s := store.NewFake()
	pub := outbox.NewFakePublisher()
	wh := webhook.New("unused-plivo-token", s, pub)
	lim := concurrency.NewFake(5)
	r := routing.NewWithLimiter(s, nil, lim)
	h := handler.New(wh, r)
	return h.Routes(), s, pub
}

// vobizStatusBody encodes a form-urlencoded Vobiz status payload.
func vobizStatusBody(callUUID, callStatus string) *bytes.Buffer {
	vals := url.Values{}
	vals.Set("CallUUID", callUUID)
	vals.Set("CallStatus", callStatus)
	vals.Set("From", "+15550001111")
	vals.Set("To", "+919999999999")
	return bytes.NewBufferString(vals.Encode())
}

// TestVobizStatusHandler_Ringing confirms the ringing status emits call.ringing.
func TestVobizStatusHandler_Ringing(t *testing.T) {
	testVobizStatusMapping(t, "ringing", "call.ringing")
}

// TestVobizStatusHandler_Answered confirms answered emits call.answered.
func TestVobizStatusHandler_Answered(t *testing.T) {
	testVobizStatusMapping(t, "answered", "call.answered")
}

// TestVobizStatusHandler_Completed confirms completed emits call.completed.
func TestVobizStatusHandler_Completed(t *testing.T) {
	testVobizStatusMapping(t, "completed", "call.completed")
}

// TestVobizStatusHandler_Failed confirms failed emits call.failed.
func TestVobizStatusHandler_Failed(t *testing.T) {
	testVobizStatusMapping(t, "failed", "call.failed")
}

// TestVobizStatusHandler_Busy confirms busy emits call.failed.
func TestVobizStatusHandler_Busy(t *testing.T) {
	testVobizStatusMapping(t, "busy", "call.failed")
}

// TestVobizStatusHandler_NoAnswer confirms no-answer emits call.failed.
func TestVobizStatusHandler_NoAnswer(t *testing.T) {
	testVobizStatusMapping(t, "no-answer", "call.failed")
}

// TestVobizStatusHandler_Initiated confirms initiated emits call.initiated.
func TestVobizStatusHandler_Initiated(t *testing.T) {
	testVobizStatusMapping(t, "initiated", "call.initiated")
}

func testVobizStatusMapping(t *testing.T, callStatus, expectedSubject string) {
	t.Helper()

	// Use warn-only mode so no signature is required.
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "true")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	routes, _, pub := buildVobizHandler(t)

	callUUID := "test-call-" + callStatus
	body := vobizStatusBody(callUUID, callStatus)

	req := httptest.NewRequest(http.MethodPost, "/wh/vobiz/status", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	msgs := pub.Messages()
	var found bool
	for _, m := range msgs {
		if m.Subject == expectedSubject {
			found = true
			break
		}
	}
	if !found {
		subjects := make([]string, len(msgs))
		for i, m := range msgs {
			subjects[i] = m.Subject
		}
		t.Fatalf("expected subject %q in published events; got: %v", expectedSubject, subjects)
	}
}

// TestVobizAnswerHandler_ReturnsXML confirms the answer handler returns valid XML.
func TestVobizAnswerHandler_ReturnsXML(t *testing.T) {
	os.Setenv("PUBLIC_WEBHOOK_BASE_URL", "https://example.ngrok.io")
	defer os.Unsetenv("PUBLIC_WEBHOOK_BASE_URL")

	routes, _, _ := buildVobizHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/wh/vobiz/answer?CallUUID=call-xyz-999", nil)
	rec := httptest.NewRecorder()

	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "xml") {
		t.Fatalf("expected XML content-type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Stream") {
		t.Fatalf("expected <Stream> in body, got: %s", body)
	}
	if !strings.Contains(body, "wss://example.ngrok.io/ws/vobiz/call-xyz-999") {
		t.Fatalf("expected wsURL in body, got: %s", body)
	}
}

// TestVobizRecordingHandler_EmitsRecordingReady confirms recording webhook stores row + emits.
func TestVobizRecordingHandler_EmitsRecordingReady(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "true")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	routes, s, pub := buildVobizHandler(t)

	vals := url.Values{}
	vals.Set("CallUUID", "call-rec-001")
	vals.Set("RecordUrl", "https://vobiz.ai/recordings/rec-001.mp3")
	vals.Set("RecordingID", "rec-001")
	vals.Set("RecordingFormat", "mp3")
	body := bytes.NewBufferString(vals.Encode())

	req := httptest.NewRequest(http.MethodPost, "/wh/vobiz/recording", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// Should have stored a recording row.
	recording, err := s.GetRecording(context.Background(), "call-rec-001")
	if err != nil {
		t.Fatalf("GetRecording: %v", err)
	}
	if recording.StorageKey != "https://vobiz.ai/recordings/rec-001.mp3" {
		t.Errorf("StorageKey: want recording URL, got %q", recording.StorageKey)
	}

	// Should have emitted call.recording.ready.
	var foundReady bool
	for _, m := range pub.Messages() {
		if m.Subject == webhook.NATSSubjectRecordingReady {
			foundReady = true
			var payload map[string]string
			if err := json.Unmarshal(m.Data, &payload); err != nil {
				t.Fatalf("decode ready event: %v", err)
			}
			if payload["provider"] != "vobiz" {
				t.Errorf("expected provider=vobiz, got %q", payload["provider"])
			}
			if payload["recording_url"] != "https://vobiz.ai/recordings/rec-001.mp3" {
				t.Errorf("unexpected recording_url: %q", payload["recording_url"])
			}
			break
		}
	}
	if !foundReady {
		t.Fatalf("expected %q in published subjects", webhook.NATSSubjectRecordingReady)
	}
}

// TestVobizAliasRoutes_AnswerAndStatus confirms alias routes /answer and /status work.
func TestVobizAliasRoutes(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "true")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")
	os.Setenv("PUBLIC_WEBHOOK_BASE_URL", "https://ngrok.example.com")
	defer os.Unsetenv("PUBLIC_WEBHOOK_BASE_URL")

	routes, _, _ := buildVobizHandler(t)

	// Test /answer alias.
	req := httptest.NewRequest(http.MethodGet, "/answer?CallUUID=call-alias-001", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/answer alias: expected 200, got %d", rec.Code)
	}

	// Test /status alias.
	body := vobizStatusBody("call-alias-002", "ringing")
	req2 := httptest.NewRequest(http.MethodPost, "/status", body)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	routes.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("/status alias: expected 204, got %d", rec2.Code)
	}

	// Test /hangup alias.
	bodyHangup := url.Values{"CallUUID": []string{"call-alias-003"}, "TenantID": []string{"tenant-alias"}}.Encode()
	req3 := httptest.NewRequest(http.MethodPost, "/hangup", bytes.NewBufferString(bodyHangup))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec3 := httptest.NewRecorder()
	routes.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("/hangup alias: expected 204, got %d", rec3.Code)
	}

	// Test /fallback alias (returns XML like /answer).
	req4 := httptest.NewRequest(http.MethodGet, "/fallback?CallUUID=call-alias-004", nil)
	rec4 := httptest.NewRecorder()
	routes.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("/fallback alias: expected 200, got %d", rec4.Code)
	}
}

// Ensure ProviderRoutingRule is usable via model import.
var _ = model.ProviderRoutingRule{}
