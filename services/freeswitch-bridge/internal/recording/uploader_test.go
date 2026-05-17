package recording_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lead/services/freeswitch-bridge/internal/recording"
)

func TestUploadPipeline(t *testing.T) {
	// Write a fake WAV file.
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "tenant-abc_sess-001.wav")
	wavData := []byte("RIFF....fake-wav-audio-data")
	if err := os.WriteFile(wavPath, wavData, 0644); err != nil {
		t.Fatalf("write wav: %v", err)
	}

	spaces := recording.NewFakeSpaces()
	pub := recording.NewFakePublisher()
	store := recording.NewFakeRecordingStore()

	worker := recording.NewWorker(spaces, pub, store)

	ctx := context.Background()
	if err := worker.Enqueue(ctx, "tenant-abc", "sess-001", wavPath); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if worker.QueueDepth() != 1 {
		t.Fatalf("expected queue depth 1, got %d", worker.QueueDepth())
	}

	errs := worker.DrainOnce(ctx)
	if len(errs) > 0 {
		t.Fatalf("drain errors: %v", errs)
	}

	// Verify file uploaded to fake spaces.
	if len(spaces.Stored) != 1 {
		t.Fatalf("expected 1 stored file, got %d", len(spaces.Stored))
	}

	// Verify SSE header.
	headers := spaces.SSEHeaders()
	if headers["x-amz-server-side-encryption"] != "aws:kms" {
		t.Errorf("expected SSE header, got: %v", headers)
	}

	// Verify store entry.
	if len(store.Uploads) != 1 {
		t.Fatalf("expected 1 upload record, got %d", len(store.Uploads))
	}
	if store.Uploads[0].TenantID != "tenant-abc" {
		t.Errorf("expected tenant-abc, got %s", store.Uploads[0].TenantID)
	}

	// Verify NATS message published.
	msgs := pub.Messages["call.recording.ready"]
	if len(msgs) != 1 {
		t.Fatalf("expected 1 NATS message on call.recording.ready, got %d", len(msgs))
	}

	// Verify local file deleted.
	if _, err := os.Stat(wavPath); !os.IsNotExist(err) {
		t.Error("expected local WAV file to be deleted after upload")
	}
}

func TestUploadPipeline_RetryOnFailure(t *testing.T) {
	// Use a path that doesn't exist → should fail and re-queue.
	spaces := recording.NewFakeSpaces()
	pub := recording.NewFakePublisher()
	store := recording.NewFakeRecordingStore()
	worker := recording.NewWorker(spaces, pub, store)

	ctx := context.Background()
	_ = worker.Enqueue(ctx, "tenant-xyz", "sess-999", "/nonexistent/path.wav")

	// DrainOnce 3 times → all fail → final error reported.
	for i := 0; i < 3; i++ {
		worker.DrainOnce(ctx)
	}

	// After 3 retries, job should be dropped (not re-queued).
	if worker.QueueDepth() != 0 {
		t.Errorf("expected queue to be empty after max retries, got %d", worker.QueueDepth())
	}
}
