package dispatcher

// RateLimiter enforces tenant-configurable hourly/daily/cost call caps per campaign.
// Keys are stored in Redis with appropriate TTLs.
// The Redis implementation mirrors the hand-rolled RESP approach in
// services/telephony-adapter/internal/concurrency/limiter.go.

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Default caps when a campaign limit is <= 0 (i.e. not configured).
const (
	DefaultHourlyCap = 50
	DefaultDailyCap  = 500
)

// RateLimiter checks and records per-campaign call budgets.
type RateLimiter interface {
	// Allowed checks whether a call may be placed for campaignID given the
	// configured caps.  Returns (true, "") when allowed;
	// (false, reason) when any cap is exceeded.
	Allowed(ctx context.Context, campaignID string, hourlyCap, dailyCap int, costCapINR float64) (bool, string)
	// RecordCall increments the hour and day counters for campaignID.
	// Call ONLY after a successful telephony 2xx — not on 503/error.
	RecordCall(ctx context.Context, campaignID string)
}

// ----- Redis-backed implementation ------------------------------------------

// RedisRateLimiter uses hand-rolled RESP to enforce rate limits stored in Redis.
type RedisRateLimiter struct {
	addr string
}

// NewRedisRateLimiter creates a limiter from a redis:// URL.
func NewRedisRateLimiter(redisURL string) *RedisRateLimiter {
	return &RedisRateLimiter{addr: parseRedisAddr(redisURL)}
}

func (r *RedisRateLimiter) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	return d.DialContext(ctx, "tcp", r.addr)
}

// hourKey returns the Redis key for hourly call count.
// Format: campaign:rate:hour:{campaign_id}:{YYYYMMDDHH}
func hourKey(campaignID string, t time.Time) string {
	return fmt.Sprintf("campaign:rate:hour:%s:%s", campaignID, t.UTC().Format("2006010215"))
}

// dayKey returns the Redis key for daily call count.
// Format: campaign:rate:day:{campaign_id}:{YYYYMMDD}
func dayKey(campaignID string, t time.Time) string {
	return fmt.Sprintf("campaign:rate:day:%s:%s", campaignID, t.UTC().Format("20060102"))
}

// costKey returns the Redis key for daily cost accumulation.
// Format: campaign:cost:day:{campaign_id}:{YYYYMMDD}
func costKey(campaignID string, t time.Time) string {
	return fmt.Sprintf("campaign:cost:day:%s:%s", campaignID, t.UTC().Format("20060102"))
}

// getCount fetches the integer value of a key (0 if missing).
func (r *RedisRateLimiter) getCount(ctx context.Context, key string) (int, error) {
	conn, err := r.dial(ctx)
	if err != nil {
		return 0, fmt.Errorf("redis dial: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	cmd := fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", len(key), key)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return 0, fmt.Errorf("redis write: %w", err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		return 0, fmt.Errorf("redis read: %w", err)
	}
	line := strings.TrimSpace(string(buf[:n]))
	if line == "$-1" || line == "" {
		return 0, nil // key does not exist
	}
	// Bulk string reply: $N\r\nVALUE\r\n  — may arrive in one read
	if strings.HasPrefix(line, "$") {
		// Parse bulk string: next line is the value
		parts := strings.SplitN(line, "\r\n", 2)
		if len(parts) == 2 {
			val, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return 0, nil
			}
			return val, nil
		}
		// Need another read for the value
		n2, err := conn.Read(buf)
		if err != nil {
			return 0, fmt.Errorf("redis read value: %w", err)
		}
		val, err := strconv.Atoi(strings.TrimSpace(string(buf[:n2])))
		if err != nil {
			return 0, nil
		}
		return val, nil
	}
	// Integer reply
	if strings.HasPrefix(line, ":") {
		val, err := strconv.Atoi(line[1:])
		return val, err
	}
	return 0, fmt.Errorf("redis unexpected reply: %s", line)
}

// incrWithExpire sends INCR then EXPIRE in a single pipeline write.
func (r *RedisRateLimiter) incrWithExpire(ctx context.Context, key string, ttlSec int) error {
	conn, err := r.dial(ctx)
	if err != nil {
		return fmt.Errorf("redis dial: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Pipeline: INCR + EXPIRE
	incrCmd := fmt.Sprintf("*2\r\n$4\r\nINCR\r\n$%d\r\n%s\r\n", len(key), key)
	expTTL := fmt.Sprintf("%d", ttlSec)
	expCmd := fmt.Sprintf("*3\r\n$6\r\nEXPIRE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
		len(key), key, len(expTTL), expTTL)

	if _, err := conn.Write([]byte(incrCmd + expCmd)); err != nil {
		return fmt.Errorf("redis pipeline write: %w", err)
	}
	// Read two replies (INCR integer + EXPIRE integer) — ignore values.
	buf := make([]byte, 256)
	_, _ = conn.Read(buf)
	return nil
}

func (r *RedisRateLimiter) Allowed(ctx context.Context, campaignID string, hourlyCap, dailyCap int, costCapINR float64) (bool, string) {
	if hourlyCap <= 0 {
		hourlyCap = DefaultHourlyCap
	}
	if dailyCap <= 0 {
		dailyCap = DefaultDailyCap
	}

	now := time.Now()

	hourCount, err := r.getCount(ctx, hourKey(campaignID, now))
	if err != nil {
		log.Printf("ratelimit: getCount hour error (fail open): %v", err)
		return true, ""
	}
	if hourCount >= hourlyCap {
		return false, fmt.Sprintf("hourly cap %d reached (count=%d)", hourlyCap, hourCount)
	}

	dayCount, err := r.getCount(ctx, dayKey(campaignID, now))
	if err != nil {
		log.Printf("ratelimit: getCount day error (fail open): %v", err)
		return true, ""
	}
	if dayCount >= dailyCap {
		return false, fmt.Sprintf("daily cap %d reached (count=%d)", dailyCap, dayCount)
	}

	// Cost cap: read current cost (populated by observability service).
	// costCapINR <= 0 means unlimited.
	if costCapINR > 0 {
		costCount, err := r.getCount(ctx, costKey(campaignID, now))
		if err != nil {
			log.Printf("ratelimit: getCount cost error (fail open): %v", err)
			return true, ""
		}
		// costCount is stored as integer paise (1 INR = 100 paise).
		if float64(costCount)/100.0 >= costCapINR {
			return false, fmt.Sprintf("cost cap %.2f INR reached (spent=%.2f INR)", costCapINR, float64(costCount)/100.0)
		}
	}

	return true, ""
}

func (r *RedisRateLimiter) RecordCall(ctx context.Context, campaignID string) {
	now := time.Now()
	if err := r.incrWithExpire(ctx, hourKey(campaignID, now), 3600); err != nil {
		log.Printf("ratelimit: RecordCall hour INCR error: %v", err)
	}
	if err := r.incrWithExpire(ctx, dayKey(campaignID, now), 86400); err != nil {
		log.Printf("ratelimit: RecordCall day INCR error: %v", err)
	}
}

// ----- No-op implementation (always allows, no-op record) -------------------

// NoopRateLimiter allows all calls and records nothing.  Used when Redis is
// not configured or in tests that don't care about rate limiting.
type NoopRateLimiter struct{}

func (n *NoopRateLimiter) Allowed(_ context.Context, _ string, _, _ int, _ float64) (bool, string) {
	return true, ""
}
func (n *NoopRateLimiter) RecordCall(_ context.Context, _ string) {}

// ----- Fake implementation (in-memory, for tests) ---------------------------

// FakeRateLimiter is a thread-safe in-memory RateLimiter for unit tests.
type FakeRateLimiter struct {
	mu        sync.Mutex
	hourCount map[string]int // key: "campaign:hour:{campaignID}:{HH}"
	dayCount  map[string]int // key: "campaign:day:{campaignID}:{day}"
}

// NewFakeRateLimiter creates a new in-memory rate limiter.
func NewFakeRateLimiter() *FakeRateLimiter {
	return &FakeRateLimiter{
		hourCount: make(map[string]int),
		dayCount:  make(map[string]int),
	}
}

func (f *FakeRateLimiter) Allowed(_ context.Context, campaignID string, hourlyCap, dailyCap int, costCapINR float64) (bool, string) {
	if hourlyCap <= 0 {
		hourlyCap = DefaultHourlyCap
	}
	if dailyCap <= 0 {
		dailyCap = DefaultDailyCap
	}
	now := time.Now()
	hk := f.hkey(campaignID, now)
	dk := f.dkey(campaignID, now)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hourCount[hk] >= hourlyCap {
		return false, fmt.Sprintf("hourly cap %d reached (count=%d)", hourlyCap, f.hourCount[hk])
	}
	if f.dayCount[dk] >= dailyCap {
		return false, fmt.Sprintf("daily cap %d reached (count=%d)", dailyCap, f.dayCount[dk])
	}
	return true, ""
}

func (f *FakeRateLimiter) RecordCall(_ context.Context, campaignID string) {
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hourCount[f.hkey(campaignID, now)]++
	f.dayCount[f.dkey(campaignID, now)]++
}

// HourCount returns the current hour count for campaignID (for test assertions).
func (f *FakeRateLimiter) HourCount(campaignID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hourCount[f.hkey(campaignID, time.Now())]
}

// DayCount returns the current day count for campaignID (for test assertions).
func (f *FakeRateLimiter) DayCount(campaignID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dayCount[f.dkey(campaignID, time.Now())]
}

// SetHourCount directly sets the hour counter (for test setup).
func (f *FakeRateLimiter) SetHourCount(campaignID string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hourCount[f.hkey(campaignID, time.Now())] = count
}

func (f *FakeRateLimiter) hkey(campaignID string, t time.Time) string {
	return fmt.Sprintf("campaign:hour:%s:%s", campaignID, t.UTC().Format("2006010215"))
}

func (f *FakeRateLimiter) dkey(campaignID string, t time.Time) string {
	return fmt.Sprintf("campaign:day:%s:%s", campaignID, t.UTC().Format("20060102"))
}
