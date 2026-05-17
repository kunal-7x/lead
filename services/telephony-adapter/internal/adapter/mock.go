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
