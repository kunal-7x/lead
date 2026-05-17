// Package recording handles pulling recordings from providers and storing them encrypted.
package recording

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/store"
)

// ObjectStore abstracts DO Spaces / S3-compatible object storage.
type ObjectStore interface {
	// Put uploads data and returns the storage key.
	// encrypted=true requests server-side encryption via the configured envelope key.
	Put(ctx context.Context, tenantID, filename string, data []byte, encrypted bool) (storageKey string, err error)
	// SSEHeaders returns the headers set for the last Put (for testing verification).
	SSEHeaders() map[string]string
}

// RecordingProvider can fetch raw audio from a telephony provider.
type RecordingProvider interface {
	FetchAudio(ctx context.Context, providerURL string) ([]byte, error)
}

// Manager orchestrates pulling a recording from a provider and uploading it to object storage.
type Manager struct {
	store    store.Store
	objStore ObjectStore
	rp       RecordingProvider
}

func New(s store.Store, obj ObjectStore, rp RecordingProvider) *Manager {
	return &Manager{store: s, objStore: obj, rp: rp}
}

// Store pulls the recording at providerURL and persists it under the tenant prefix with SSE.
func (m *Manager) Store(ctx context.Context, sessionID, tenantID, providerCallID, providerURL string) (*model.CallRecording, error) {
	data, err := m.rp.FetchAudio(ctx, providerURL)
	if err != nil {
		return nil, fmt.Errorf("recording: fetch audio: %w", err)
	}

	filename := fmt.Sprintf("%s/%s.mp3", tenantID, providerCallID)
	storageKey, err := m.objStore.Put(ctx, tenantID, filename, data, true /* encrypted */)
	if err != nil {
		return nil, fmt.Errorf("recording: upload: %w", err)
	}

	rec := &model.CallRecording{
		ID:             fmt.Sprintf("rec-%s", providerCallID),
		SessionID:      sessionID,
		ProviderCallID: providerCallID,
		StorageKey:     storageKey,
		Encrypted:      true,
		SizeBytes:      int64(len(data)),
		CreatedAt:      time.Now(),
	}
	if err := m.store.StoreRecording(ctx, rec); err != nil {
		return nil, fmt.Errorf("recording: store: %w", err)
	}
	return rec, nil
}

// FakeObjectStore is a test-double for DO Spaces that records calls and simulates SSE headers.
type FakeObjectStore struct {
	Stored     map[string][]byte
	lastHeaders map[string]string
}

func NewFakeObjectStore() *FakeObjectStore {
	return &FakeObjectStore{Stored: make(map[string][]byte)}
}

func (f *FakeObjectStore) Put(_ context.Context, tenantID, filename string, data []byte, encrypted bool) (string, error) {
	key := fmt.Sprintf("do-spaces://%s/%s", tenantID, filename)
	cp := make([]byte, len(data))
	copy(cp, data)
	f.Stored[key] = cp
	headers := map[string]string{
		"x-amz-server-side-encryption": "aws:kms",
	}
	if encrypted {
		headers["x-amz-server-side-encryption-aws-kms-key-id"] = "vault-envelope-key"
	}
	f.lastHeaders = headers
	return key, nil
}

func (f *FakeObjectStore) SSEHeaders() map[string]string {
	return f.lastHeaders
}

// FakeRecordingProvider returns synthetic audio bytes.
type FakeRecordingProvider struct {
	AudioData []byte
}

func NewFakeRecordingProvider() *FakeRecordingProvider {
	return &FakeRecordingProvider{AudioData: []byte("fake-audio-data")}
}

func (f *FakeRecordingProvider) FetchAudio(_ context.Context, providerURL string) ([]byte, error) {
	if providerURL == "" {
		return nil, fmt.Errorf("recording provider: empty URL")
	}
	return f.AudioData, nil
}
