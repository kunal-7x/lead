package adapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/esl"
	"github.com/lead/services/telephony-adapter/internal/model"
)

func makeFS(fake *esl.FakeClient) *adapter.FreeSWITCHAdapter {
	return adapter.NewFreeSWITCH(fake, "/tmp/recordings", "ws://voice-agent:8765/audio")
}

func TestFreeSWITCH_PlaceCall(t *testing.T) {
	fake := esl.NewFakeClient()
	fs := makeFS(fake)

	id, err := fs.PlaceCall(context.Background(), model.CallRequest{
		SessionID: "sess-001",
		TenantID:  "tenant-abc",
		ToNumber:  "+919876543210",
		MaxDuration: 300,
	})
	if err != nil {
		t.Fatalf("PlaceCall: %v", err)
	}
	if string(id) == "" {
		t.Fatal("expected non-empty call UUID")
	}
	if !strings.HasPrefix(string(fake.LastCommand()), "originate") {
		t.Errorf("expected originate command, got: %s", fake.LastCommand())
	}
}

func TestFreeSWITCH_Hangup(t *testing.T) {
	fake := esl.NewFakeClient()
	fs := makeFS(fake)

	// First place a call
	id, err := fs.PlaceCall(context.Background(), model.CallRequest{
		SessionID: "sess-002",
		TenantID:  "tenant-abc",
		ToNumber:  "+919876543210",
		MaxDuration: 300,
	})
	if err != nil {
		t.Fatalf("PlaceCall: %v", err)
	}

	if err := fs.Hangup(context.Background(), id); err != nil {
		t.Fatalf("Hangup: %v", err)
	}
	if !fake.WasHungUp(string(id)) {
		t.Errorf("expected call %s to be hung up", id)
	}
}

func TestFreeSWITCH_GetRecording(t *testing.T) {
	fake := esl.NewFakeClient()
	fs := makeFS(fake)

	ref, err := fs.GetRecording(context.Background(), model.ProviderCallID("test-uuid-123"))
	if err != nil {
		t.Fatalf("GetRecording: %v", err)
	}
	if ref.StorageKey == "" {
		t.Fatal("expected non-empty storage key")
	}
	if !ref.Encrypted {
		t.Fatal("expected recording to be marked encrypted")
	}
	if !strings.Contains(ref.StorageKey, "test-uuid-123") {
		t.Errorf("storage key should contain the call UUID, got: %s", ref.StorageKey)
	}
}

func TestFreeSWITCH_Healthy(t *testing.T) {
	fake := esl.NewFakeClient()
	fs := makeFS(fake)

	if !fs.Healthy(context.Background()) {
		t.Fatal("expected healthy")
	}

	fake.SetHealthy(false)
	if fs.Healthy(context.Background()) {
		t.Fatal("expected unhealthy after SetHealthy(false)")
	}
}

func TestFreeSWITCH_ImplementsInterface(t *testing.T) {
	// Compile-time check that FreeSWITCHAdapter satisfies the Telephony interface.
	var _ adapter.Telephony = (*adapter.FreeSWITCHAdapter)(nil)
}
