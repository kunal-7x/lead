package adapter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// MockEvent records an action taken by the Mock adapter.
type MockEvent struct {
	Action string
	CallID model.ProviderCallID
	At     time.Time
}

// Mock is an in-memory Telephony implementation for tests and demos.
type Mock struct {
	mu        sync.Mutex
	calls     map[model.ProviderCallID]model.CallRequest
	events    []MockEvent
	healthy   bool
	counter   int
	states    []string
	audioLoop [][]byte
}

func NewMock() *Mock {
	return &Mock{
		calls:   make(map[model.ProviderCallID]model.CallRequest),
		healthy: true,
	}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) SetHealthy(v bool) {
	m.mu.Lock()
	m.healthy = v
	m.mu.Unlock()
}

func (m *Mock) Healthy(_ context.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

func (m *Mock) PlaceCall(_ context.Context, req model.CallRequest) (model.ProviderCallID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter++
	id := model.ProviderCallID(fmt.Sprintf("mock-call-%d", m.counter))
	m.calls[id] = req
	m.events = append(m.events, MockEvent{Action: "place", CallID: id, At: time.Now()})
	if len(m.states) == 0 {
		m.states = []string{"queued", "ringing", "answered", "completed"}
	}
	return id, nil
}

func (m *Mock) Hangup(_ context.Context, id model.ProviderCallID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.calls[id]; !ok {
		return fmt.Errorf("mock: call %s not found", id)
	}
	m.events = append(m.events, MockEvent{Action: "hangup", CallID: id, At: time.Now()})
	return nil
}

func (m *Mock) TransferToHuman(_ context.Context, id model.ProviderCallID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.calls[id]; !ok {
		return fmt.Errorf("mock: call %s not found", id)
	}
	m.events = append(m.events, MockEvent{Action: "transfer", CallID: id, At: time.Now()})
	return nil
}

func (m *Mock) GetRecording(_ context.Context, id model.ProviderCallID) (model.RecordingRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.calls[id]; !ok {
		return model.RecordingRef{}, fmt.Errorf("mock: call %s not found", id)
	}
	ref := model.RecordingRef{
		RecordingID: fmt.Sprintf("rec-%s", id),
		StorageKey:  fmt.Sprintf("mock/recordings/%s.mp3", id),
		Encrypted:   true,
	}
	m.events = append(m.events, MockEvent{Action: "get_recording", CallID: id, At: time.Now()})
	return ref, nil
}

// Events returns a copy of all recorded events.
func (m *Mock) Events() []MockEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockEvent, len(m.events))
	copy(out, m.events)
	return out
}

// CallCount returns the number of placed calls.
func (m *Mock) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *Mock) SetScriptedStates(states ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = append([]string(nil), states...)
}

func (m *Mock) ScriptedStates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.states))
	copy(out, m.states)
	return out
}

func (m *Mock) SetAudioLoop(frames ...[]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audioLoop = make([][]byte, len(frames))
	for i, frame := range frames {
		m.audioLoop[i] = append([]byte(nil), frame...)
	}
}

func (m *Mock) AudioLoop() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.audioLoop) == 0 {
		return [][]byte{make([]byte, 320), make([]byte, 320)}
	}
	out := make([][]byte, len(m.audioLoop))
	for i, frame := range m.audioLoop {
		out[i] = append([]byte(nil), frame...)
	}
	return out
}
