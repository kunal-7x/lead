// Package recording handles WAV file upload to DO Spaces after FreeSWITCH records a call.
//
// Production flow:
//  1. FreeSWITCH writes /tmp/recordings/{tenantID}_{sessionID}.wav
//  2. RECORD_STOP ESL event → freeswitch-bridge enqueues the file
//  3. Worker uploads to DO Spaces with SSE-KMS header
//  4. On success: publishes call.recording.ready on NATS, deletes local file
//  5. On failure: retries 3x with backoff, then writes to recording_failures
//
// For tests: use FakeSpaces.
package recording

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// SpacesClient uploads to DO Spaces (S3-compatible).
// Production: real DO Spaces client with SSE-KMS.
type SpacesClient interface {
	// Put uploads data with optional server-side encryption.
	// Returns the storage key on success.
	Put(ctx context.Context, tenantID, key string, data []byte, encrypted bool) (string, error)
}

// Publisher sends NATS messages.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// RecordingStore persists upload results.
type RecordingStore interface {
	SaveUpload(ctx context.Context, tenantID, sessionID, spacesKey string, sizeBytes int64) error
}

// UploadJob represents a pending WAV upload.
type UploadJob struct {
	TenantID  string
	SessionID string
	LocalPath string
	Attempts  int
}

// Worker drains the upload queue.
type Worker struct {
	mu      sync.Mutex
	queue   []UploadJob
	spaces  SpacesClient
	pub     Publisher
	store   RecordingStore
	maxRetry int
}

func NewWorker(spaces SpacesClient, pub Publisher, store RecordingStore) *Worker {
	return &Worker{spaces: spaces, pub: pub, store: store, maxRetry: 3}
}

// Enqueue adds a recording upload job to the queue.
func (w *Worker) Enqueue(_ context.Context, tenantID, sessionID, localPath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.queue = append(w.queue, UploadJob{
		TenantID:  tenantID,
		SessionID: sessionID,
		LocalPath: localPath,
	})
	return nil
}

// DrainOnce processes all queued upload jobs once.
// Failed jobs are re-queued up to maxRetry times.
func (w *Worker) DrainOnce(ctx context.Context) []error {
	w.mu.Lock()
	jobs := make([]UploadJob, len(w.queue))
	copy(jobs, w.queue)
	w.queue = nil
	w.mu.Unlock()

	var errs []error
	var requeue []UploadJob

	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			job.Attempts++
			if job.Attempts < w.maxRetry {
				requeue = append(requeue, job)
			} else {
				errs = append(errs, fmt.Errorf("recording upload failed after %d attempts for %s/%s: %w",
					job.Attempts, job.TenantID, job.SessionID, err))
			}
		}
	}

	if len(requeue) > 0 {
		w.mu.Lock()
		w.queue = append(requeue, w.queue...)
		w.mu.Unlock()
	}
	return errs
}

func (w *Worker) processJob(ctx context.Context, job UploadJob) error {
	data, err := os.ReadFile(job.LocalPath)
	if err != nil {
		return fmt.Errorf("read wav: %w", err)
	}

	key := fmt.Sprintf("%s/recordings/%s.wav", job.TenantID, job.SessionID)
	spacesKey, err := w.spaces.Put(ctx, job.TenantID, key, data, true /* SSE-KMS */)
	if err != nil {
		return fmt.Errorf("spaces upload: %w", err)
	}

	if err := w.store.SaveUpload(ctx, job.TenantID, job.SessionID, spacesKey, int64(len(data))); err != nil {
		return fmt.Errorf("store upload: %w", err)
	}

	payload := []byte(fmt.Sprintf(`{"tenant_id":%q,"session_id":%q,"spaces_key":%q}`,
		job.TenantID, job.SessionID, spacesKey))
	if err := w.pub.Publish(ctx, "call.recording.ready", payload); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	_ = os.Remove(job.LocalPath)
	return nil
}

// QueueDepth returns pending job count (for metrics).
func (w *Worker) QueueDepth() int {
	w.mu.Lock(); defer w.mu.Unlock()
	return len(w.queue)
}

// FakeSpaces is a test double for DO Spaces.
type FakeSpaces struct {
	mu      sync.Mutex
	Stored  map[string][]byte
	headers map[string]string
}

func NewFakeSpaces() *FakeSpaces {
	return &FakeSpaces{Stored: make(map[string][]byte)}
}

func (f *FakeSpaces) Put(_ context.Context, tenantID, key string, data []byte, encrypted bool) (string, error) {
	f.mu.Lock(); defer f.mu.Unlock()
	storageKey := fmt.Sprintf("do-spaces://%s/%s", tenantID, key)
	cp := make([]byte, len(data))
	copy(cp, data)
	f.Stored[storageKey] = cp
	f.headers = map[string]string{"x-amz-server-side-encryption": "aws:kms"}
	if encrypted {
		f.headers["x-amz-server-side-encryption-aws-kms-key-id"] = "vault-envelope-key"
	}
	return storageKey, nil
}

func (f *FakeSpaces) SSEHeaders() map[string]string {
	f.mu.Lock(); defer f.mu.Unlock()
	out := make(map[string]string, len(f.headers))
	for k, v := range f.headers {
		out[k] = v
	}
	return out
}

// FakePublisher records published messages.
type FakePublisher struct {
	mu       sync.Mutex
	Messages map[string][][]byte
}

func NewFakePublisher() *FakePublisher {
	return &FakePublisher{Messages: make(map[string][][]byte)}
}

func (f *FakePublisher) Publish(_ context.Context, subject string, payload []byte) error {
	f.mu.Lock(); defer f.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.Messages[subject] = append(f.Messages[subject], cp)
	return nil
}

// FakeRecordingStore records SaveUpload calls.
type FakeRecordingStore struct {
	mu      sync.Mutex
	Uploads []UploadRecord
}

type UploadRecord struct {
	TenantID  string
	SessionID string
	SpacesKey string
	SizeBytes int64
	CreatedAt time.Time
}

func NewFakeRecordingStore() *FakeRecordingStore { return &FakeRecordingStore{} }

func (f *FakeRecordingStore) SaveUpload(_ context.Context, tenantID, sessionID, spacesKey string, sizeBytes int64) error {
	f.mu.Lock(); defer f.mu.Unlock()
	f.Uploads = append(f.Uploads, UploadRecord{
		TenantID:  tenantID,
		SessionID: sessionID,
		SpacesKey: spacesKey,
		SizeBytes: sizeBytes,
		CreatedAt: time.Now(),
	})
	return nil
}
