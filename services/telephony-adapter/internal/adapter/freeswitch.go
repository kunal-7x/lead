package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/lead/services/telephony-adapter/internal/esl"
	"github.com/lead/services/telephony-adapter/internal/model"
)

// FreeSWITCHAdapter implements Telephony using FreeSWITCH ESL.
// For tests: inject esl.NewFakeClient().
// For production: inject esl.Dial(ctx, addr, password).
type FreeSWITCHAdapter struct {
	client    esl.Client
	recordDir string // local recording dir, e.g. /tmp/recordings
	wsURL     string // voice agent WebSocket URL
}

func NewFreeSWITCH(client esl.Client, recordDir, voiceAgentWSURL string) *FreeSWITCHAdapter {
	return &FreeSWITCHAdapter{
		client:    client,
		recordDir: recordDir,
		wsURL:     voiceAgentWSURL,
	}
}

func (f *FreeSWITCHAdapter) Name() string { return "freeswitch" }

// PlaceCall sends an ESL originate command and returns the call UUID.
// Production: dials through Jio SIP trunk configured in external.xml.
func (f *FreeSWITCHAdapter) PlaceCall(ctx context.Context, req model.CallRequest) (model.ProviderCallID, error) {
	uuid := req.SessionID
	cmd := esl.Command(fmt.Sprintf(
		"originate {origination_uuid=%s,max_call_duration=%d,recording_file=%s/%s_%s.wav}sofia/gateway/jio/%s &socket(%s async full)",
		uuid, req.MaxDuration, f.recordDir, req.TenantID, req.SessionID, req.ToNumber, f.wsURL,
	))
	resp, err := f.client.SendCommand(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("freeswitch: place call: %w", err)
	}
	if !strings.HasPrefix(resp.Body, "+OK") {
		return "", fmt.Errorf("freeswitch: originate failed: %s", resp.Body)
	}
	// ESL returns "+OK <uuid>"
	parts := strings.Fields(resp.Body)
	if len(parts) >= 2 {
		return model.ProviderCallID(parts[1]), nil
	}
	return model.ProviderCallID(uuid), nil
}

// Hangup sends ESL uuid_kill.
func (f *FreeSWITCHAdapter) Hangup(ctx context.Context, id model.ProviderCallID) error {
	resp, err := f.client.SendCommand(ctx, esl.Command("uuid_kill "+string(id)))
	if err != nil {
		return fmt.Errorf("freeswitch: hangup: %w", err)
	}
	if !strings.HasPrefix(resp.Body, "+OK") {
		return fmt.Errorf("freeswitch: hangup failed: %s", resp.Body)
	}
	return nil
}

// TransferToHuman bridges the call to a human agent via ESL uuid_transfer.
func (f *FreeSWITCHAdapter) TransferToHuman(ctx context.Context, id model.ProviderCallID, targetNumber string) error {
	cmd := esl.Command(fmt.Sprintf("uuid_transfer %s %s XML default", string(id), targetNumber))
	resp, err := f.client.SendCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("freeswitch: transfer: %w", err)
	}
	if !strings.HasPrefix(resp.Body, "+OK") {
		return fmt.Errorf("freeswitch: transfer failed: %s", resp.Body)
	}
	return nil
}

// GetRecording returns the DO Spaces key for the recording.
// The recording upload worker (freeswitch-bridge service) writes the key
// to the store after upload; this returns it by convention.
func (f *FreeSWITCHAdapter) GetRecording(_ context.Context, id model.ProviderCallID) (model.RecordingRef, error) {
	// Convention: freeswitch-bridge uploads to {tenantID}/recordings/{sessionID}.wav
	// The caller (placeCall) set session ID = uuid prefix; use it as-is here.
	return model.RecordingRef{
		RecordingID: string(id),
		StorageKey:  fmt.Sprintf("recordings/%s.wav", string(id)),
		Encrypted:   true,
	}, nil
}

// Healthy pings FreeSWITCH via ESL status command.
func (f *FreeSWITCHAdapter) Healthy(ctx context.Context) bool {
	return f.client.Healthy(ctx)
}
