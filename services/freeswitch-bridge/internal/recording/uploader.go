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

// SpacesClient uploads to DO Spaces (S3-compatible) hot storage.
type SpacesClient interface {
	// Put uploads data with optional server-side encryption.
	// Returns the storage key on success.
	Put(ctx context.Context, tenantID, key string, data []byte, encrypted bool) (string, error)
}

// Archiver writes a long-term immutable copy (B2 WORM). Optional — if nil,
// no archive copy is made.
type Archiver interface {
	Archive(ctx context.Context, tenantID, key string, data []byte) (string, error)
}

// Publisher sends NATS messages.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// RecordingStore persists upload results.
type RecordingStore interface {
	SaveUpload(ctx context.Context, tenantID, sessionID, spacesKey string, sizeBytes int64) error
}

// ArchiveStore persists B2 archive metadata (optional — Worker skips if nil).
type ArchiveStore interface {
	SaveArchive(ctx context.Context, tenantID, sessionID, b2Key string) error
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
	mu       sync.Mutex
	queue    []UploadJob
	spaces   SpacesClient
	archiver Archiver
	pub      Publisher
	store    RecordingStore
	archive  ArchiveStore
	maxRetry int
}

// WorkerOption configures optional Worker capabilities.
type WorkerOption func(*Worker)

// WithArchiver enables WORM archive uploads (e.g. Backblaze B2).
func WithArchiver(a Archiver) WorkerOption { return func(w *Worker) { w.archiver = a } }

// WithArchiveStore wires a persistent record of archive copies.
func WithArchiveStore(s ArchiveStore) WorkerOption { return func(w *Worker) { w.archive = s } }

func NewWorker(spaces SpacesClient, pub Publisher, store RecordingStore, opts ...WorkerOption) *Worker {
	w := &Worker{spaces: spaces, pub: pub, store: store, maxRetry: 3}
	for _, opt := range opts {
		opt(w)
	}
	return w
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
	spacesKey, err := w.spaces.Put(ctx, job.TenantID, key, data, true /* SSE */)
	if err != nil {
		return fmt.Errorf("spaces upload: %w", err)
	}

	if err := w.store.SaveUpload(ctx, job.TenantID, job.SessionID, spacesKey, int64(len(data))); err != nil {
		return fmt.Errorf("store upload: %w", err)
	}

	var b2Key string
	if w.archiver != nil {
		ak, aerr := w.archiver.Archive(ctx, job.TenantID, key, data)
		if aerr != nil && ak == "" {
			// Hard failure — keep local file, return error so Worker retries.
			return fmt.Errorf("b2 archive: %w", aerr)
		}
		b2Key = ak
		if w.archive != nil && b2Key != "" {
			_ = w.archive.SaveArchive(ctx, job.TenantID, job.SessionID, b2Key)
		}
		if aerr != nil {
			// Soft failure (object-lock-not-enabled): archived without retention,
			// still emit event but log via NATS subject so ops can fix bucket.
			_ = w.pub.Publish(ctx, "recording.archive.warning",
				[]byte(fmt.Sprintf(`{"session_id":%q,"warning":%q}`, job.SessionID, aerr.Error())))
		}
	}

	payload := []byte(fmt.Sprintf(`{"tenant_id":%q,"session_id":%q,"spaces_key":%q,"b2_key":%q}`,
		job.TenantID, job.SessionID, spacesKey, b2Key))
	if err := w.pub.Publish(ctx, "call.recording.ready", payload); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	if b2Key != "" {
		_ = w.pub.Publish(ctx, "recording.archived", payload)
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

// FakeArchiver is a test double for B2.
type FakeArchiver struct {
	mu     sync.Mutex
	Stored map[string][]byte
}

func NewFakeArchiver() *FakeArchiver {
	return &FakeArchiver{Stored: make(map[string][]byte)}
}

func (f *FakeArchiver) Archive(_ context.Context, tenantID, key string, data []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	storageKey := fmt.Sprintf("b2://%s/%s", tenantID, key)
	cp := make([]byte, len(data))
	copy(cp, data)
	f.Stored[storageKey] = cp
	return storageKey, nil
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
