package telephony_adapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
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

// TestVobizAnswerHandler_CallUUIDFromBody confirms Bug E fix: the answer handler
// reads CallUUID from the POST body (form-encoded), not just the query string, so
// the emitted <Stream> URL carries the real call id instead of "unknown".
func TestVobizAnswerHandler_CallUUIDFromBody(t *testing.T) {
	os.Setenv("PUBLIC_WEBHOOK_BASE_URL", "https://example.ngrok.io")
	defer os.Unsetenv("PUBLIC_WEBHOOK_BASE_URL")

	routes, _, _ := buildVobizHandler(t)

	vals := url.Values{}
	vals.Set("CallUUID", "body-call-123")
	vals.Set("From", "+15550001111")
	vals.Set("To", "+919999999999")
	req := httptest.NewRequest(http.MethodPost, "/wh/vobiz/answer", bytes.NewBufferString(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "/ws/vobiz/unknown") {
		t.Fatalf("regression: emitted /ws/vobiz/unknown, got: %s", body)
	}
	if !strings.Contains(body, "wss://example.ngrok.io/ws/vobiz/body-call-123") {
		t.Fatalf("expected ws URL with body CallUUID, got: %s", body)
	}
}

// TestVobizAnswerHandler_CallUUIDFromJSONBody confirms the JSON-body variant.
func TestVobizAnswerHandler_CallUUIDFromJSONBody(t *testing.T) {
	os.Setenv("PUBLIC_WEBHOOK_BASE_URL", "https://example.ngrok.io")
	defer os.Unsetenv("PUBLIC_WEBHOOK_BASE_URL")

	routes, _, _ := buildVobizHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/wh/vobiz/answer",
		bytes.NewBufferString(`{"call_uuid":"json-call-777"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/ws/vobiz/json-call-777") {
		t.Fatalf("expected ws URL with JSON call_uuid, got: %s", rec.Body.String())
	}
}

// TestVobizConcurrency_HangupFreesSlot_NoTenantInWebhook confirms Bug F fix:
// the hangup webhook (which carries only CallUUID, no tenant) frees the
// concurrency slot by resolving the tenant from the stored session, so we don't
// hit the cap forever after VOBIZ_MAX_CONCURRENT_CALLS calls.
func TestVobizConcurrency_HangupFreesSlot_NoTenantInWebhook(t *testing.T) {
	os.Setenv("VOBIZ_WEBHOOK_VERIFY", "true")
	defer os.Unsetenv("VOBIZ_WEBHOOK_VERIFY")

	const max = 2
	s := store.NewFake()
	s.ResetRoutingRules()
	s.AddRoutingRule(model.ProviderRoutingRule{
		ID: "rule-vobiz", ProviderID: "vobiz", Priority: 1, MaxFailRate: 1.0,
	})

	lim := concurrency.NewFake(max)
	vobizAdapter := newFakeVobizAdapter()
	r := routing.NewWithLimiter(s, []adapter.Telephony{vobizAdapter}, lim)
	pub := outbox.NewFakePublisher()
	wh := webhook.New("unused", s, pub)
	routes := handler.New(wh, r).Routes()

	ctx := context.Background()
	tenant := "tenant-hangup"

	// Place calls up to the cap.
	uuids := []string{"uuid-1", "uuid-2"}
	for i, u := range uuids {
		vobizAdapter.nextUUID = u
		if _, err := r.PlaceCall(ctx, model.CallRequest{
			SessionID:  fmt.Sprintf("sess-%d", i),
			TenantID:   tenant,
			ToNumber:   "+91999",
		}); err != nil {
			t.Fatalf("place call %d: %v", i+1, err)
		}
	}
	if lim.Count(tenant) != max {
		t.Fatalf("expected count=%d, got %d", max, lim.Count(tenant))
	}

	// Hang up the first call via webhook carrying ONLY CallUUID (no tenant).
	hbody := url.Values{"CallUUID": []string{"uuid-1"}}.Encode()
	hreq := httptest.NewRequest(http.MethodPost, "/wh/vobiz/hangup", bytes.NewBufferString(hbody))
	hreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hrec := httptest.NewRecorder()
	routes.ServeHTTP(hrec, hreq)
	if hrec.Code != http.StatusNoContent {
		t.Fatalf("hangup: expected 204, got %d", hrec.Code)
	}

	if lim.Count(tenant) != max-1 {
		t.Fatalf("after hangup expected count=%d, got %d", max-1, lim.Count(tenant))
	}

	// A duplicate terminal signal (status=completed) for the SAME call must NOT
	// double-decrement.
	sbody := url.Values{"CallUUID": []string{"uuid-1"}, "CallStatus": []string{"completed"}}.Encode()
	sreq := httptest.NewRequest(http.MethodPost, "/wh/vobiz/status", bytes.NewBufferString(sbody))
	sreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srec := httptest.NewRecorder()
	routes.ServeHTTP(srec, sreq)
	if srec.Code != http.StatusNoContent {
		t.Fatalf("status: expected 204, got %d", srec.Code)
	}
	if lim.Count(tenant) != max-1 {
		t.Fatalf("after duplicate terminal event expected count=%d (no double-decrement), got %d", max-1, lim.Count(tenant))
	}

	// Freed slot means a new call can be placed.
	vobizAdapter.nextUUID = "uuid-3"
	if _, err := r.PlaceCall(ctx, model.CallRequest{SessionID: "sess-3", TenantID: tenant, ToNumber: "+91999"}); err != nil {
		t.Fatalf("expected slot freed, got: %v", err)
	}
}

// fakeVobizAdapter is a minimal vobiz-named Telephony adapter for handler tests.
type fakeVobizAdapter struct{ nextUUID string }

func newFakeVobizAdapter() *fakeVobizAdapter { return &fakeVobizAdapter{nextUUID: "uuid-x"} }

func (f *fakeVobizAdapter) Name() string                  { return "vobiz" }
func (f *fakeVobizAdapter) Healthy(_ context.Context) bool { return true }
func (f *fakeVobizAdapter) PlaceCall(_ context.Context, _ model.CallRequest) (model.ProviderCallID, error) {
	return model.ProviderCallID(f.nextUUID), nil
}
func (f *fakeVobizAdapter) Hangup(_ context.Context, _ model.ProviderCallID) error { return nil }
func (f *fakeVobizAdapter) TransferToHuman(_ context.Context, _ model.ProviderCallID, _ string) error {
	return nil
}
func (f *fakeVobizAdapter) GetRecording(_ context.Context, _ model.ProviderCallID) (model.RecordingRef, error) {
	return model.RecordingRef{}, nil
}

// Ensure ProviderRoutingRule is usable via model import.
var _ = model.ProviderRoutingRule{}
