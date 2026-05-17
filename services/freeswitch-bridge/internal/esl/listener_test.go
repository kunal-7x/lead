package esl_test

import (
	"context"
	"sync"
	"testing"

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
	f.mu.Lock(); defer f.mu.Unlock()
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
	f.mu.Lock(); defer f.mu.Unlock()
	f.started[uuid] = true
	return nil
}

func (f *fakeSessionStore) SetSessionEnded(_ context.Context, uuid string) error {
	f.mu.Lock(); defer f.mu.Unlock()
	f.ended[uuid] = true
	return nil
}

type fakeUploader struct {
	mu    sync.Mutex
	queue []string
}

func (f *fakeUploader) Enqueue(_ context.Context, tenantID, sessionID, localPath string) error {
	f.mu.Lock(); defer f.mu.Unlock()
	f.queue = append(f.queue, localPath)
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
	if len(pub.messages["call.session.started"]) != 1 {
		t.Errorf("expected 1 NATS message, got %d", len(pub.messages["call.session.started"]))
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
	if uploader.queue[0] != "/tmp/recordings/tenant-abc_sess-003.wav" {
		t.Errorf("unexpected path: %s", uploader.queue[0])
	}
}
