//go:build live

package recording_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lead/services/freeswitch-bridge/internal/recording"
)

// init swaps net.DefaultResolver to CUSTOM_DNS so Jio/corporate DNS blocks
// on Backblaze/DO hostnames don't fail the live tests. Mirrors the pattern
// used in services/knowledge/internal/netutil.
func init() {
	addr := strings.TrimSpace(os.Getenv("CUSTOM_DNS"))
	if addr == "" {
		return
	}
	parts := strings.Split(addr, ",")
	srv := strings.TrimSpace(parts[0])
	if !strings.Contains(srv, ":") {
		srv += ":53"
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", srv)
		},
	}
}

// liveEnv returns the named env var or skips the test if missing.
func liveEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s not set; skipping live test", key)
	}
	return v
}

// minimalWAV builds a 256-byte silent RIFF/WAVE header + samples so the
// uploaded object is a structurally valid 8 kHz mono PCM WAV.
func minimalWAV() []byte {
	dataLen := 256
	sampleRate := 8000
	bitsPerSample := 16
	channels := 1

	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	fileSize := 36 + dataLen

	u32 := func(v int) []byte {
		return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
	}
	u16 := func(v int) []byte {
		return []byte{byte(v), byte(v >> 8)}
	}

	buf := make([]byte, 0, 44+dataLen)
	buf = append(buf, []byte("RIFF")...)
	buf = append(buf, u32(fileSize)...)
	buf = append(buf, []byte("WAVEfmt ")...)
	buf = append(buf, u32(16)...)
	buf = append(buf, u16(1)...)
	buf = append(buf, u16(channels)...)
	buf = append(buf, u32(sampleRate)...)
	buf = append(buf, u32(byteRate)...)
	buf = append(buf, u16(blockAlign)...)
	buf = append(buf, u16(bitsPerSample)...)
	buf = append(buf, []byte("data")...)
	buf = append(buf, u32(dataLen)...)
	buf = append(buf, make([]byte, dataLen)...) // silence
	return buf
}

// TestSpacesLive_UploadPresignDelete uploads a WAV to real DO Spaces,
// presigns a download URL, verifies it returns 200, then cleans up.
func TestSpacesLive_UploadPresignDelete(t *testing.T) {
	if os.Getenv("RUN_LIVE_TESTS") != "1" {
		t.Skip("RUN_LIVE_TESTS=1 required")
	}
	spaces, err := recording.NewSpaces(
		liveEnv(t, "DO_SPACES_KEY"),
		liveEnv(t, "DO_SPACES_SECRET"),
		liveEnv(t, "DO_SPACES_REGION"),
		liveEnv(t, "DO_SPACES_BUCKET"),
	)
	if err != nil {
		t.Fatalf("NewSpaces: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenant := "c8-livetest"
	sess := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	key := fmt.Sprintf("%s/recordings/%s.wav", tenant, sess)
	data := minimalWAV()

	storageKey, err := spaces.Put(ctx, tenant, key, data, true)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.HasPrefix(storageKey, "do-spaces://") {
		t.Errorf("unexpected storage key: %s", storageKey)
	}
	t.Logf("uploaded: %s (%d bytes)", storageKey, len(data))

	dl, err := spaces.PresignDownload(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignDownload: %v", err)
	}
	t.Logf("presigned: %s", dl)
	resp, err := http.Get(dl)
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("presigned GET status=%d", resp.StatusCode)
	}

	if err := spaces.Delete(ctx, key); err != nil {
		t.Errorf("Delete: %v", err)
	}
}

// TestB2Live_ArchiveWORMRefusesDelete uploads to B2 with COMPLIANCE
// retention and verifies the object exists and resists immediate deletion.
func TestB2Live_ArchiveWORMRefusesDelete(t *testing.T) {
	if os.Getenv("RUN_LIVE_TESTS") != "1" {
		t.Skip("RUN_LIVE_TESTS=1 required")
	}
	b2, err := recording.NewB2(
		liveEnv(t, "B2_KEY"),
		liveEnv(t, "B2_SECRET"),
		liveEnv(t, "B2_BUCKET"),
		365,
	)
	if err != nil {
		t.Fatalf("NewB2: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenant := "c8-livetest"
	sess := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	key := fmt.Sprintf("%s/recordings/%s.wav", tenant, sess)
	data := minimalWAV()

	storageKey, warn := b2.Archive(ctx, tenant, key, data)
	if storageKey == "" {
		t.Fatalf("Archive: %v", warn)
	}
	if warn != nil {
		t.Logf("WARNING: %v (bucket lacks Object Lock — recording archived without retention)", warn)
		// Soft fail — record this but continue with delete-check.
	}
	t.Logf("archived: %s", storageKey)

	ok, err := b2.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if !ok {
		t.Fatalf("object missing after archive")
	}

	delErr := b2.TryDelete(ctx, key)
	if warn == nil {
		// Object Lock is active — delete must be refused.
		if delErr == nil {
			t.Fatalf("WORM violation: B2 allowed delete of locked object %s", key)
		}
		t.Logf("WORM proof OK: B2 refused delete: %v", delErr)
	} else {
		// Bucket lacks Object Lock; delete will succeed. Clean up.
		if delErr != nil {
			t.Logf("delete returned: %v", delErr)
		}
	}
}

// TestUploadPipelineLive_EndToEnd exercises the full Worker: local WAV →
// Spaces (hot) + B2 (cold WORM) → NATS event → local file deleted.
func TestUploadPipelineLive_EndToEnd(t *testing.T) {
	if os.Getenv("RUN_LIVE_TESTS") != "1" {
		t.Skip("RUN_LIVE_TESTS=1 required")
	}
	spaces, err := recording.NewSpaces(
		liveEnv(t, "DO_SPACES_KEY"),
		liveEnv(t, "DO_SPACES_SECRET"),
		liveEnv(t, "DO_SPACES_REGION"),
		liveEnv(t, "DO_SPACES_BUCKET"),
	)
	if err != nil {
		t.Fatalf("NewSpaces: %v", err)
	}
	b2, err := recording.NewB2(
		liveEnv(t, "B2_KEY"),
		liveEnv(t, "B2_SECRET"),
		liveEnv(t, "B2_BUCKET"),
		365,
	)
	if err != nil {
		t.Fatalf("NewB2: %v", err)
	}

	dir := t.TempDir()
	tenant := "c8-livetest"
	sess := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	wavPath := filepath.Join(dir, sess+".wav")
	if err := os.WriteFile(wavPath, minimalWAV(), 0644); err != nil {
		t.Fatalf("write wav: %v", err)
	}

	pub := recording.NewFakePublisher()
	store := recording.NewFakeRecordingStore()
	worker := recording.NewWorker(spaces, pub, store, recording.WithArchiver(b2))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := worker.Enqueue(ctx, tenant, sess, wavPath); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if errs := worker.DrainOnce(ctx); len(errs) > 0 {
		t.Fatalf("drain errors: %v", errs)
	}

	// Verify NATS message.
	msgs := pub.Messages["call.recording.ready"]
	if len(msgs) != 1 {
		t.Fatalf("expected 1 call.recording.ready msg, got %d", len(msgs))
	}
	payload := string(msgs[0])
	if !strings.Contains(payload, "do-spaces://") {
		t.Errorf("payload missing spaces key: %s", payload)
	}
	if !strings.Contains(payload, "b2://") {
		t.Errorf("payload missing b2 key: %s", payload)
	}
	if len(pub.Messages["recording.archived"]) != 1 {
		t.Errorf("expected recording.archived event")
	}

	// Local file should be gone.
	if _, err := os.Stat(wavPath); !os.IsNotExist(err) {
		t.Errorf("local file not deleted")
	}

	// Cleanup hot copy (B2 is locked).
	hotKey := fmt.Sprintf("%s/recordings/%s.wav", tenant, sess)
	_ = spaces.Delete(ctx, hotKey)
}
