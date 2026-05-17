// Package esl listens for FreeSWITCH events via the Event Socket Layer.
//
// Events handled:
//   - CHANNEL_ANSWER  → publish call.session.started on NATS
//   - CHANNEL_HANGUP  → update session status, trigger recording upload
//   - RECORD_STOP     → upload WAV to DO Spaces, publish call.recording.ready
//
// Production: connect to FreeSWITCH ESL on port 8021.
// Tests: use FakeEventSource.
package esl

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// EventType is a FreeSWITCH event name.
type EventType string

const (
	EventChannelAnswer EventType = "CHANNEL_ANSWER"
	EventChannelHangup EventType = "CHANNEL_HANGUP"
	EventRecordStop    EventType = "RECORD_STOP"
)

// Event is a parsed FreeSWITCH event.
type Event struct {
	Type      EventType
	CallUUID  string
	ExtraVars map[string]string
}

// Publisher sends events downstream (NATS in production, fake in tests).
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// SessionStore tracks active call sessions.
type SessionStore interface {
	SetSessionStarted(ctx context.Context, callUUID string) error
	SetSessionEnded(ctx context.Context, callUUID string) error
}

// RecordingUploader queues WAV files for upload.
type RecordingUploader interface {
	Enqueue(ctx context.Context, tenantID, sessionID, localPath string) error
}

// Listener processes FreeSWITCH events.
type Listener struct {
	pub      Publisher
	sessions SessionStore
	uploader RecordingUploader
}

func NewListener(pub Publisher, sessions SessionStore, uploader RecordingUploader) *Listener {
	return &Listener{pub: pub, sessions: sessions, uploader: uploader}
}

// Handle processes one FreeSWITCH event.
func (l *Listener) Handle(ctx context.Context, evt Event) error {
	switch evt.Type {
	case EventChannelAnswer:
		if err := l.sessions.SetSessionStarted(ctx, evt.CallUUID); err != nil {
			return fmt.Errorf("listener: session started: %w", err)
		}
		payload := []byte(fmt.Sprintf(`{"call_uuid":%q}`, evt.CallUUID))
		return l.pub.Publish(ctx, "call.session.started", payload)

	case EventChannelHangup:
		if err := l.sessions.SetSessionEnded(ctx, evt.CallUUID); err != nil {
			return fmt.Errorf("listener: session ended: %w", err)
		}

	case EventRecordStop:
		localPath := evt.ExtraVars["Record-File-Path"]
		tenantID, sessionID := parseFilename(localPath)
		if err := l.uploader.Enqueue(ctx, tenantID, sessionID, localPath); err != nil {
			return fmt.Errorf("listener: enqueue recording: %w", err)
		}
	}
	return nil
}

// parseFilename extracts tenantID and sessionID from the WAV filename pattern:
// /tmp/recordings/{tenantID}_{sessionID}.wav
func parseFilename(path string) (tenantID, sessionID string) {
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	base = strings.TrimSuffix(base, ".wav")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "unknown", base
}

// FakeEventSource generates synthetic FreeSWITCH events for tests.
type FakeEventSource struct {
	mu     sync.Mutex
	events []Event
}

func NewFakeEventSource() *FakeEventSource { return &FakeEventSource{} }

func (f *FakeEventSource) Push(evt Event) {
	f.mu.Lock(); defer f.mu.Unlock()
	f.events = append(f.events, evt)
}

func (f *FakeEventSource) DrainAll(ctx context.Context, l *Listener) error {
	f.mu.Lock()
	evts := make([]Event, len(f.events))
	copy(evts, f.events)
	f.events = nil
	f.mu.Unlock()
	for _, evt := range evts {
		if err := l.Handle(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}
