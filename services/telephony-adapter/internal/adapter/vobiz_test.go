package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/model"
)

// fakeVobizHTTPClient is a test double for VobizHTTPClient.
type fakeVobizHTTPClient struct {
	// Configurable responses.
	createCallUUID string
	createCallErr  error
	hangupErr      error
	transferErr    error
	recordingURL   string
	recordingErr   error
	healthy        bool

	// Captured call arguments.
	lastCreateFrom      string
	lastCreateTo        string
	lastCreateAnswerURL string
	lastHangupUUID      string
	lastTransferUUID    string
	lastTransferTarget  string
	lastRecordingUUID   string
	checkHealthCalled   bool
	checkHealthAuthID   string
}

func (f *fakeVobizHTTPClient) CreateCall(_ context.Context, _, _, from, to, answerURL string) (string, error) {
	f.lastCreateFrom = from
	f.lastCreateTo = to
	f.lastCreateAnswerURL = answerURL
	return f.createCallUUID, f.createCallErr
}

func (f *fakeVobizHTTPClient) HangupCall(_ context.Context, _, _, callUUID string) error {
	f.lastHangupUUID = callUUID
	return f.hangupErr
}

func (f *fakeVobizHTTPClient) TransferCall(_ context.Context, _, _, callUUID, target string) error {
	f.lastTransferUUID = callUUID
	f.lastTransferTarget = target
	return f.transferErr
}

func (f *fakeVobizHTTPClient) GetRecordingURL(_ context.Context, _, _, callUUID string) (string, error) {
	f.lastRecordingUUID = callUUID
	return f.recordingURL, f.recordingErr
}

func (f *fakeVobizHTTPClient) CheckHealth(_ context.Context, authID string) bool {
	f.checkHealthCalled = true
	f.checkHealthAuthID = authID
	return f.healthy
}

func TestVobizAdapter_Name(t *testing.T) {
	a := adapter.NewVobiz("aid", "tok", &fakeVobizHTTPClient{})
	if got := a.Name(); got != "vobiz" {
		t.Errorf("Name(): want %q, got %q", "vobiz", got)
	}
}

func TestVobizAdapter_Healthy(t *testing.T) {
	tests := []struct {
		name    string
		healthy bool
	}{
		{"healthy", true},
		{"unhealthy", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := &fakeVobizHTTPClient{healthy: tc.healthy}
			a := adapter.NewVobiz("myAuthID", "tok", fk)
			got := a.Healthy(context.Background())
			if got != tc.healthy {
				t.Errorf("Healthy(): want %v, got %v", tc.healthy, got)
			}
			if !fk.checkHealthCalled {
				t.Error("expected CheckHealth to be called")
			}
			if fk.checkHealthAuthID != "myAuthID" {
				t.Errorf("CheckHealth authID: want %q, got %q", "myAuthID", fk.checkHealthAuthID)
			}
		})
	}
}

func TestVobizAdapter_PlaceCall(t *testing.T) {
	tests := []struct {
		name       string
		req        model.CallRequest
		clientUUID string
		clientErr  error
		wantID     model.ProviderCallID
		wantErr    bool
	}{
		{
			name: "success",
			req: model.CallRequest{
				FromNumber:  "+15551234567",
				ToNumber:    "+919999999999",
				CallbackURL: "https://example.com/answer",
			},
			clientUUID: "vbz-call-001",
			wantID:     "vbz-call-001",
		},
		{
			name: "client error propagated",
			req: model.CallRequest{
				FromNumber:  "+15551234567",
				ToNumber:    "+919999999999",
				CallbackURL: "https://example.com/answer",
			},
			clientErr: errors.New("network failure"),
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := &fakeVobizHTTPClient{createCallUUID: tc.clientUUID, createCallErr: tc.clientErr}
			a := adapter.NewVobiz("aid", "tok", fk)
			id, err := a.PlaceCall(context.Background(), tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("PlaceCall: %v", err)
			}
			if id != tc.wantID {
				t.Errorf("ProviderCallID: want %q, got %q", tc.wantID, id)
			}
			// Verify that CallbackURL is passed as the answer URL.
			if fk.lastCreateAnswerURL != tc.req.CallbackURL {
				t.Errorf("answer_url: want %q, got %q", tc.req.CallbackURL, fk.lastCreateAnswerURL)
			}
			if fk.lastCreateFrom != tc.req.FromNumber {
				t.Errorf("from: want %q, got %q", tc.req.FromNumber, fk.lastCreateFrom)
			}
			if fk.lastCreateTo != tc.req.ToNumber {
				t.Errorf("to: want %q, got %q", tc.req.ToNumber, fk.lastCreateTo)
			}
		})
	}
}

func TestVobizAdapter_Hangup(t *testing.T) {
	tests := []struct {
		name      string
		callID    model.ProviderCallID
		clientErr error
		wantErr   bool
	}{
		{"success", "uuid-001", nil, false},
		{"error propagated", "uuid-001", errors.New("already ended"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := &fakeVobizHTTPClient{hangupErr: tc.clientErr}
			a := adapter.NewVobiz("aid", "tok", fk)
			err := a.Hangup(context.Background(), tc.callID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Hangup: %v", err)
			}
			if fk.lastHangupUUID != string(tc.callID) {
				t.Errorf("HangupCall uuid: want %q, got %q", string(tc.callID), fk.lastHangupUUID)
			}
		})
	}
}

func TestVobizAdapter_TransferToHuman(t *testing.T) {
	tests := []struct {
		name         string
		callID       model.ProviderCallID
		targetNumber string
		clientErr    error
		wantErr      bool
	}{
		{"success", "uuid-transfer-1", "https://example.com/bridge", nil, false},
		{"error propagated", "uuid-transfer-2", "https://example.com/bridge", errors.New("transfer failed"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := &fakeVobizHTTPClient{transferErr: tc.clientErr}
			a := adapter.NewVobiz("aid", "tok", fk)
			err := a.TransferToHuman(context.Background(), tc.callID, tc.targetNumber)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("TransferToHuman: %v", err)
			}
			if fk.lastTransferUUID != string(tc.callID) {
				t.Errorf("TransferCall uuid: want %q, got %q", string(tc.callID), fk.lastTransferUUID)
			}
			if fk.lastTransferTarget != tc.targetNumber {
				t.Errorf("TransferCall target: want %q, got %q", tc.targetNumber, fk.lastTransferTarget)
			}
		})
	}
}

func TestVobizAdapter_GetRecording(t *testing.T) {
	tests := []struct {
		name         string
		callID       model.ProviderCallID
		recordingURL string
		clientErr    error
		wantRef      model.RecordingRef
		wantErr      bool
	}{
		{
			name:         "success",
			callID:       "uuid-rec-1",
			recordingURL: "https://recordings.vobiz.ai/rec-1.mp3",
			wantRef: model.RecordingRef{
				RecordingID: "vobiz-uuid-rec-1",
				StorageKey:  "https://recordings.vobiz.ai/rec-1.mp3",
				Encrypted:   false,
			},
		},
		{
			name:      "error propagated",
			callID:    "uuid-rec-2",
			clientErr: errors.New("no recordings"),
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := &fakeVobizHTTPClient{recordingURL: tc.recordingURL, recordingErr: tc.clientErr}
			a := adapter.NewVobiz("aid", "tok", fk)
			ref, err := a.GetRecording(context.Background(), tc.callID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetRecording: %v", err)
			}
			if ref.RecordingID != tc.wantRef.RecordingID {
				t.Errorf("RecordingID: want %q, got %q", tc.wantRef.RecordingID, ref.RecordingID)
			}
			if ref.StorageKey != tc.wantRef.StorageKey {
				t.Errorf("StorageKey: want %q, got %q", tc.wantRef.StorageKey, ref.StorageKey)
			}
			if ref.Encrypted != tc.wantRef.Encrypted {
				t.Errorf("Encrypted: want %v, got %v", tc.wantRef.Encrypted, ref.Encrypted)
			}
			if fk.lastRecordingUUID != string(tc.callID) {
				t.Errorf("GetRecordingURL uuid: want %q, got %q", string(tc.callID), fk.lastRecordingUUID)
			}
		})
	}
}

// TestVobizAdapter_ImplementsTelephony is a compile-time check that vobiz satisfies
// the Telephony interface.
func TestVobizAdapter_ImplementsTelephony(t *testing.T) {
	var _ adapter.Telephony = adapter.NewVobiz("aid", "tok", &fakeVobizHTTPClient{})
}
