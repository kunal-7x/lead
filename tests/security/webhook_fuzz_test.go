package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func TestWebhookSignatureFuzzRejectsTamperedPayloads(t *testing.T) {
	providers := []string{"plivo", "meta_wa", "fb_lead_ads", "google_lead_forms"}
	for _, provider := range providers {
		secret := "secret-" + provider
		for i := 0; i < 1000; i++ {
			payload := []byte(fmt.Sprintf(`{"provider":%q,"id":%d,"phone":"+919876543210"}`, provider, i))
			signature := sign(secret, payload)
			tampered := append([]byte(nil), payload...)
			tampered[len(tampered)-2] ^= byte(i%19 + 1)
			if verify(secret, tampered, signature) {
				t.Fatalf("%s accepted tampered payload %d", provider, i)
			}
		}
	}
}

func TestWebhookReplayWindowAndIdempotency(t *testing.T) {
	cache := newReplayCache(5 * time.Minute)
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	if !cache.accept("evt-1", now) {
		t.Fatal("first event rejected")
	}
	if cache.accept("evt-1", now.Add(time.Minute)) {
		t.Fatal("replay accepted inside replay window")
	}
	if cache.accept("evt-2", now.Add(-10*time.Minute)) {
		t.Fatal("stale event accepted outside replay window")
	}
}

func sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func verify(secret string, payload []byte, signature string) bool {
	return hmac.Equal([]byte(sign(secret, payload)), []byte(signature))
}

type replayCache struct {
	window time.Duration
	seen   map[string]time.Time
}

func newReplayCache(window time.Duration) *replayCache {
	return &replayCache{window: window, seen: map[string]time.Time{}}
}

func (r *replayCache) accept(id string, at time.Time) bool {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	if at.Before(now.Add(-r.window)) {
		return false
	}
	if _, ok := r.seen[id]; ok {
		return false
	}
	r.seen[id] = at
	return true
}
