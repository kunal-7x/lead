package telephony_adapter_test

import (
	"context"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/model"
)

// TestMock_PlacesCall verifies that the mock adapter returns a call ID and records the event.
func TestMock_PlacesCall(t *testing.T) {
	m := adapter.NewMock()
	ctx := context.Background()

	id, err := m.PlaceCall(ctx, model.CallRequest{
		SessionID:  "sess-1",
		TenantID:   "tenant-a",
		FromNumber: "+19001234567",
		ToNumber:   "+919876543210",
	})
	if err != nil {
		t.Fatalf("PlaceCall: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty call ID")
	}
	if m.CallCount() != 1 {
		t.Fatalf("expected 1 placed call, got %d", m.CallCount())
	}

	evts := m.Events()
	if len(evts) != 1 || evts[0].Action != "place" {
		t.Fatalf("expected [place] event, got %v", evts)
	}
}

// TestMock_EmitsExpectedEventSequence verifies place → hangup produces the correct ordered events.
func TestMock_EmitsExpectedEventSequence(t *testing.T) {
	m := adapter.NewMock()
	ctx := context.Background()

	id, _ := m.PlaceCall(ctx, model.CallRequest{ToNumber: "+91999"})
	if err := m.Hangup(ctx, id); err != nil {
		t.Fatalf("Hangup: %v", err)
	}

	evts := m.Events()
	if len(evts) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evts))
	}
	if evts[0].Action != "place" {
		t.Fatalf("expected first event to be 'place', got %q", evts[0].Action)
	}
	if evts[1].Action != "hangup" {
		t.Fatalf("expected second event to be 'hangup', got %q", evts[1].Action)
	}
}

// TestMock_TransferToHuman verifies the transfer event is recorded.
func TestMock_TransferToHuman(t *testing.T) {
	m := adapter.NewMock()
	ctx := context.Background()

	id, _ := m.PlaceCall(ctx, model.CallRequest{ToNumber: "+91888"})
	if err := m.TransferToHuman(ctx, id, "+911234567890"); err != nil {
		t.Fatalf("TransferToHuman: %v", err)
	}

	evts := m.Events()
	if len(evts) != 2 || evts[1].Action != "transfer" {
		t.Fatalf("expected [place, transfer] events, got %v", evts)
	}
}

// TestMock_GetRecording verifies a recording reference is returned with encrypted flag.
func TestMock_GetRecording(t *testing.T) {
	m := adapter.NewMock()
	ctx := context.Background()

	id, _ := m.PlaceCall(ctx, model.CallRequest{ToNumber: "+91777"})
	ref, err := m.GetRecording(ctx, id)
	if err != nil {
		t.Fatalf("GetRecording: %v", err)
	}
	if ref.RecordingID == "" {
		t.Fatal("expected non-empty RecordingID")
	}
	if !ref.Encrypted {
		t.Fatal("expected recording to be marked encrypted")
	}

	evts := m.Events()
	last := evts[len(evts)-1]
	if last.Action != "get_recording" {
		t.Fatalf("expected last event to be 'get_recording', got %q", last.Action)
	}
}

// TestMock_HangupUnknownCall verifies an error is returned for an unknown call ID.
func TestMock_HangupUnknownCall(t *testing.T) {
	m := adapter.NewMock()
	err := m.Hangup(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for unknown call ID, got nil")
	}
}

// TestMock_HealthToggle verifies Healthy() returns the correct value after SetHealthy.
func TestMock_HealthToggle(t *testing.T) {
	m := adapter.NewMock()
	if !m.Healthy(context.Background()) {
		t.Fatal("expected new Mock to be healthy")
	}
	m.SetHealthy(false)
	if m.Healthy(context.Background()) {
		t.Fatal("expected Mock to be unhealthy after SetHealthy(false)")
	}
}
