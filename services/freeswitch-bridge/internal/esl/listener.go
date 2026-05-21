// Package esl listens for FreeSWITCH events via the Event Socket Layer.
package esl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lead/libs/go/events"
)

// EventType is a FreeSWITCH Event-Name.
type EventType string

const (
	EventChannelCreate         EventType = "CHANNEL_CREATE"
	EventChannelAnswer         EventType = "CHANNEL_ANSWER"
	EventChannelHangup         EventType = "CHANNEL_HANGUP"
	EventChannelHangupComplete EventType = "CHANNEL_HANGUP_COMPLETE"
	EventRecordStop            EventType = "RECORD_STOP"
	EventCustom                EventType = "CUSTOM"
)

// Event is a parsed FreeSWITCH event.
type Event struct {
	Type      EventType
	CallUUID  string
	ExtraVars map[string]string
	Body      []byte
}

// FromHeaders builds an Event from one ESL plain event message.
func FromHeaders(headers map[string]string, body []byte) Event {
	ev := Event{
		Type:      EventType(headers["Event-Name"]),
		CallUUID:  firstHeader(headers, "Unique-ID", "Channel-Unique-ID", "Channel-Call-UUID", "variable_uuid"),
		ExtraVars: cloneMap(headers),
		Body:      append([]byte(nil), body...),
	}
	return ev
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
	now      func() time.Time
}

func NewListener(pub Publisher, sessions SessionStore, uploader RecordingUploader) *Listener {
	return &Listener{
		pub:      pub,
		sessions: sessions,
		uploader: uploader,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Handle processes one FreeSWITCH event.
func (l *Listener) Handle(ctx context.Context, evt Event) error {
	if evt.ExtraVars == nil {
		evt.ExtraVars = map[string]string{}
	}
	if evt.CallUUID == "" {
		evt.CallUUID = firstHeader(evt.ExtraVars, "Unique-ID", "Channel-Unique-ID", "Channel-Call-UUID", "variable_uuid")
	}

	switch evt.Type {
	case EventChannelAnswer:
		if l.sessions != nil {
			if err := l.sessions.SetSessionStarted(ctx, evt.CallUUID); err != nil {
				return fmt.Errorf("listener: session started: %w", err)
			}
		}
	case EventChannelHangup, EventChannelHangupComplete:
		if l.sessions != nil {
			if err := l.sessions.SetSessionEnded(ctx, evt.CallUUID); err != nil {
				return fmt.Errorf("listener: session ended: %w", err)
			}
		}
	case EventRecordStop:
		if l.uploader != nil {
			recordingPath := recordingPath(evt)
			if recordingPath != "" {
				tenantID, sessionID := recordingIDs(evt, recordingPath)
				if err := l.uploader.Enqueue(ctx, tenantID, sessionID, recordingPath); err != nil {
					return fmt.Errorf("listener: enqueue recording: %w", err)
				}
			}
		}
	}

	subject, ok := subjectForEvent(evt)
	if !ok {
		return nil
	}
	payload, err := json.Marshal(l.normalized(evt))
	if err != nil {
		return fmt.Errorf("listener: marshal event: %w", err)
	}
	if err := l.pub.Publish(ctx, subject, payload); err != nil {
		return fmt.Errorf("listener: publish %s: %w", subject, err)
	}
	return nil
}

type NormalizedEvent struct {
	EventName      string            `json:"event_name"`
	EventSubclass  string            `json:"event_subclass,omitempty"`
	CallUUID       string            `json:"call_uuid,omitempty"`
	TenantID       string            `json:"tenant_id,omitempty"`
	SessionID      string            `json:"session_id,omitempty"`
	Caller         string            `json:"caller,omitempty"`
	Destination    string            `json:"destination,omitempty"`
	ProviderCallID string            `json:"provider_call_id,omitempty"`
	RecordingPath  string            `json:"recording_path,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
}

func (l *Listener) normalized(evt Event) NormalizedEvent {
	tenantID, sessionID := recordingIDs(evt, recordingPath(evt))
	return NormalizedEvent{
		EventName:      string(evt.Type),
		EventSubclass:  firstHeader(evt.ExtraVars, "Event-Subclass"),
		CallUUID:       evt.CallUUID,
		TenantID:       tenantID,
		SessionID:      sessionID,
		Caller:         firstHeader(evt.ExtraVars, "Caller-Caller-ID-Number", "Caller-ANI", "Caller-Orig-Caller-ID-Number", "variable_effective_caller_id_number"),
		Destination:    firstHeader(evt.ExtraVars, "Caller-Destination-Number", "variable_destination_number"),
		ProviderCallID: firstHeader(evt.ExtraVars, "variable_provider_call_id", "variable_sip_call_id", "variable_plivo_call_uuid", "variable_exotel_call_sid", "sip_call_id"),
		RecordingPath:  recordingPath(evt),
		Timestamp:      l.now(),
		Headers:        cloneMap(evt.ExtraVars),
		Body:           string(evt.Body),
	}
}

func subjectForEvent(evt Event) (string, bool) {
	switch evt.Type {
	case EventChannelCreate:
		return events.SubjectFreeSwitchChannelCreated, true
	case EventChannelAnswer:
		return events.SubjectFreeSwitchChannelAnswered, true
	case EventChannelHangup, EventChannelHangupComplete:
		return events.SubjectFreeSwitchChannelHangup, true
	case EventRecordStop:
		return events.SubjectFreeSwitchRecordingStopped, true
	case EventCustom:
		subclass := firstHeader(evt.ExtraVars, "Event-Subclass")
		return audioStreamSubject(subclass)
	default:
		return "", false
	}
}

func audioStreamSubject(subclass string) (string, bool) {
	if !strings.HasPrefix(subclass, "mod_audio_stream::") {
		return "", false
	}
	name := strings.TrimPrefix(subclass, "mod_audio_stream::")
	switch {
	case strings.Contains(name, "connect"), strings.Contains(name, "start"):
		return events.SubjectFreeSwitchAudioStreamStarted, true
	case strings.Contains(name, "disconnect"), strings.Contains(name, "stop"):
		return events.SubjectFreeSwitchAudioStreamStopped, true
	case strings.Contains(name, "error"):
		return events.SubjectFreeSwitchAudioStreamError, true
	default:
		return events.SubjectFreeSwitchAudioStreamEvent, true
	}
}

func recordingPath(evt Event) string {
	return firstHeader(evt.ExtraVars,
		"Record-File-Path",
		"record_file",
		"variable_record_file",
		"variable_record_session_path",
		"variable_recording_path",
	)
}

func recordingIDs(evt Event, path string) (tenantID, sessionID string) {
	tenantID = firstHeader(evt.ExtraVars, "variable_tenant_id", "tenant_id", "Tenant-ID")
	sessionID = firstHeader(evt.ExtraVars, "variable_session_id", "session_id", "Session-ID")
	if tenantID != "" && sessionID != "" {
		return tenantID, sessionID
	}
	fileTenant, fileSession := parseFilename(path)
	if tenantID == "" {
		tenantID = fileTenant
	}
	if sessionID == "" {
		sessionID = firstNonEmpty(fileSession, evt.CallUUID)
	}
	return tenantID, sessionID
}

// parseFilename extracts tenantID and sessionID from:
// /tmp/recordings/{tenantID}_{sessionID}.wav
func parseFilename(path string) (tenantID, sessionID string) {
	base := strings.ReplaceAll(path, "\\", "/")
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.TrimSuffix(base, ".wav")
	base = strings.TrimSuffix(base, ".WAV")
	if base == "" {
		return "unknown", ""
	}
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "unknown", base
}

func firstHeader(headers map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(headers[key]); v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// FakeEventSource generates synthetic FreeSWITCH events for tests.
type FakeEventSource struct {
	mu     sync.Mutex
	events []Event
}

func NewFakeEventSource() *FakeEventSource { return &FakeEventSource{} }

func (f *FakeEventSource) Push(evt Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
