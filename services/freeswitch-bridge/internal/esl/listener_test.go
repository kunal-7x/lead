package esl_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/freeswitch-bridge/internal/esl"
)

// --- fakes ---

type fakePublisher struct {
	mu       sync.Mutex
	messages map[string][][]byte
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{messages: make(map[string][][]byte)}
}

func (f *fakePublisher) Publish(_ context.Context, subject string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.messages[subject] = append(f.messages[subject], cp)
	return nil
}

type fakeSessionStore struct {
	mu      sync.Mutex
	started map[string]bool
	ended   map[string]bool
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{started: make(map[string]bool), ended: make(map[string]bool)}
}

func (f *fakeSessionStore) SetSessionStarted(_ context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started[uuid] = true
	return nil
}

func (f *fakeSessionStore) SetSessionEnded(_ context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended[uuid] = true
	return nil
}

type fakeUploader struct {
	mu    sync.Mutex
	queue []queuedUpload
}

type queuedUpload struct {
	tenantID  string
	sessionID string
	path      string
}

func (f *fakeUploader) Enqueue(_ context.Context, tenantID, sessionID, localPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, queuedUpload{tenantID: tenantID, sessionID: sessionID, path: localPath})
	return nil
}

// --- tests ---

func TestListener_ChannelAnswer(t *testing.T) {
	pub := newFakePublisher()
	sessions := newFakeSessionStore()
	uploader := &fakeUploader{}
	l := esl.NewListener(pub, sessions, uploader)

	src := esl.NewFakeEventSource()
	src.Push(esl.Event{Type: esl.EventChannelAnswer, CallUUID: "call-001"})
	if err := src.DrainAll(context.Background(), l); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !sessions.started["call-001"] {
		t.Error("expected session marked started")
	}
	if len(pub.messages[events.SubjectFreeSwitchChannelAnswered]) != 1 {
		t.Errorf("expected 1 NATS message, got %d", len(pub.messages[events.SubjectFreeSwitchChannelAnswered]))
	}
}

func TestListener_ChannelHangup(t *testing.T) {
	pub := newFakePublisher()
	sessions := newFakeSessionStore()
	uploader := &fakeUploader{}
	l := esl.NewListener(pub, sessions, uploader)

	src := esl.NewFakeEventSource()
	src.Push(esl.Event{Type: esl.EventChannelHangup, CallUUID: "call-002"})
	if err := src.DrainAll(context.Background(), l); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !sessions.ended["call-002"] {
		t.Error("expected session marked ended")
	}
}

func TestListener_RecordStop(t *testing.T) {
	pub := newFakePublisher()
	sessions := newFakeSessionStore()
	uploader := &fakeUploader{}
	l := esl.NewListener(pub, sessions, uploader)

	src := esl.NewFakeEventSource()
	src.Push(esl.Event{
		Type:     esl.EventRecordStop,
		CallUUID: "call-003",
		ExtraVars: map[string]string{
			"Record-File-Path": "/tmp/recordings/tenant-abc_sess-003.wav",
		},
	})
	if err := src.DrainAll(context.Background(), l); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(uploader.queue) != 1 {
		t.Fatalf("expected 1 upload queued, got %d", len(uploader.queue))
	}
	if uploader.queue[0].path != "/tmp/recordings/tenant-abc_sess-003.wav" {
		t.Errorf("unexpected path: %s", uploader.queue[0].path)
	}
	if uploader.queue[0].tenantID != "tenant-abc" || uploader.queue[0].sessionID != "sess-003" {
		t.Errorf("unexpected recording IDs: %#v", uploader.queue[0])
	}
	if len(pub.messages[events.SubjectFreeSwitchRecordingStopped]) != 1 {
		t.Fatalf("expected recording stopped event")
	}
}

func TestListener_ChannelCreatePayloadMapping(t *testing.T) {
	pub := newFakePublisher()
	l := esl.NewListener(pub, newFakeSessionStore(), &fakeUploader{})

	src := esl.NewFakeEventSource()
	src.Push(esl.Event{
		Type:     esl.EventChannelCreate,
		CallUUID: "call-004",
		ExtraVars: map[string]string{
			"variable_tenant_id":        "tenant-1",
			"variable_session_id":       "session-1",
			"Caller-Caller-ID-Number":   "+15551234567",
			"Caller-Destination-Number": "1000",
			"variable_sip_call_id":      "sip-abc",
		},
	})
	if err := src.DrainAll(context.Background(), l); err != nil {
		t.Fatalf("drain: %v", err)
	}
	msgs := pub.messages[events.SubjectFreeSwitchChannelCreated]
	if len(msgs) != 1 {
		t.Fatalf("expected one channel.created message, got %d", len(msgs))
	}
	var payload map[string]any
	if err := json.Unmarshal(msgs[0], &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["tenant_id"] != "tenant-1" || payload["session_id"] != "session-1" {
		t.Fatalf("unexpected payload IDs: %#v", payload)
	}
	if payload["provider_call_id"] != "sip-abc" {
		t.Fatalf("unexpected provider_call_id: %#v", payload)
	}
}

func TestListener_AudioStreamCustomSubject(t *testing.T) {
	pub := newFakePublisher()
	l := esl.NewListener(pub, newFakeSessionStore(), &fakeUploader{})

	err := l.Handle(context.Background(), esl.Event{
		Type:     esl.EventCustom,
		CallUUID: "call-005",
		ExtraVars: map[string]string{
			"Event-Subclass": "mod_audio_stream::connect",
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(pub.messages[events.SubjectFreeSwitchAudioStreamStarted]) != 1 {
		t.Fatalf("expected audio stream started event")
	}
}
