// Package concurrency provides a per-tenant concurrency cap for outbound calls,
// backed by Redis INCR/DECR. When REDIS_URL is unset the limiter is a no-op so
// the service can run in dev/test without Redis.
package concurrency

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrConcurrencyLimit is returned when the per-tenant call cap is reached.
var ErrConcurrencyLimit = errors.New("concurrency: per-tenant call limit reached")

// Limiter enforces a maximum number of concurrent calls per tenant.
type Limiter interface {
	// Incr increments the counter for tenantID. Returns ErrConcurrencyLimit
	// if the new value would exceed max. On limit hit the counter is decremented back.
	Incr(ctx context.Context, tenantID string) error
	// Decr decrements the counter for tenantID (called on hangup/completed).
	Decr(ctx context.Context, tenantID string)
}

// New returns a Redis-backed Limiter when REDIS_URL is set, otherwise a no-op.
// max is the maximum concurrent calls per tenant (e.g. from VOBIZ_MAX_CONCURRENT_CALLS).
func New(max int) Limiter {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Println("concurrency: REDIS_URL not set; concurrency cap disabled (no-op)")
		return &noopLimiter{}
	}
	return &redisLimiter{
		addr: parseRedisAddr(redisURL),
		max:  max,
	}
}

// NewWithAddr creates a Redis-backed limiter with an explicit address (for tests).
func NewWithAddr(addr string, max int) Limiter {
	return &redisLimiter{addr: addr, max: max}
}

// NewNoop returns a no-op limiter (always allows, for tests without Redis).
func NewNoop() Limiter { return &noopLimiter{} }

// NewFake returns an injectable in-memory limiter suitable for unit tests.
func NewFake(max int) *FakeLimiter { return &FakeLimiter{max: max} }

// ----- no-op -----

type noopLimiter struct{}

func (n *noopLimiter) Incr(_ context.Context, _ string) error { return nil }
func (n *noopLimiter) Decr(_ context.Context, _ string)       {}

// ----- fake (in-memory, for tests) -----

// FakeLimiter is a thread-safe in-memory limiter for unit tests.
// It does not need Redis.
type FakeLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	max     int
	Incrted []string // recorded tenant IDs for assertions
}

func (f *FakeLimiter) Incr(_ context.Context, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[tenantID]++
	f.Incrted = append(f.Incrted, tenantID)
	if f.counts[tenantID] > f.max {
		f.counts[tenantID]-- // roll back
		return ErrConcurrencyLimit
	}
	return nil
}

func (f *FakeLimiter) Decr(_ context.Context, tenantID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		return
	}
	if f.counts[tenantID] > 0 {
		f.counts[tenantID]--
	}
}

// Count returns the current counter for tenantID (for test assertions).
func (f *FakeLimiter) Count(tenantID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[tenantID]
}

// ----- Redis-backed -----

// redisLimiter implements Limiter via a minimal hand-rolled Redis INCR/DECR client.
// We keep it dependency-free (no go-redis) to avoid adding module deps.
type redisLimiter struct {
	addr string
	max  int
}

func (r *redisLimiter) key(tenantID string) string {
	return fmt.Sprintf("vobiz:concurrency:%s", tenantID)
}

func (r *redisLimiter) Incr(ctx context.Context, tenantID string) error {
	val, err := r.incrKey(ctx, r.key(tenantID))
	if err != nil {
		// Redis unavailable → fail open (no-op) so we don't block calls.
		log.Printf("concurrency: redis INCR error (fail open): %v", err)
		return nil
	}
	if val > r.max {
		// Exceeded: roll back and reject.
		_ = r.decrKey(ctx, r.key(tenantID))
		return ErrConcurrencyLimit
	}
	return nil
}

func (r *redisLimiter) Decr(_ context.Context, tenantID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.decrKey(ctx, r.key(tenantID)); err != nil {
		log.Printf("concurrency: redis DECR error: %v", err)
	}
}

func (r *redisLimiter) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	return d.DialContext(ctx, "tcp", r.addr)
}

func (r *redisLimiter) incrKey(ctx context.Context, key string) (int, error) {
	conn, err := r.dial(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	cmd := fmt.Sprintf("*2\r\n$4\r\nINCR\r\n$%d\r\n%s\r\n", len(key), key)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return 0, err
	}
	return readRedisInt(conn)
}

func (r *redisLimiter) decrKey(ctx context.Context, key string) error {
	conn, err := r.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	cmd := fmt.Sprintf("*2\r\n$4\r\nDECR\r\n$%d\r\n%s\r\n", len(key), key)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}
	_, err = readRedisInt(conn)
	return err
}

// readRedisInt reads a Redis integer reply (:N\r\n).
func readRedisInt(conn net.Conn) (int, error) {
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(string(buf[:n]))
	if len(line) == 0 {
		return 0, fmt.Errorf("redis: empty reply")
	}
	if line[0] == '-' {
		return 0, fmt.Errorf("redis: error: %s", line[1:])
	}
	if line[0] == ':' {
		v, err := strconv.Atoi(line[1:])
		return v, err
	}
	return 0, fmt.Errorf("redis: unexpected reply: %s", line)
}

// parseRedisAddr extracts host:port from REDIS_URL.
// Supports: redis://host:port, redis://:password@host:port, host:port.
func parseRedisAddr(u string) string {
	u = strings.TrimPrefix(u, "redis://")
	if idx := strings.LastIndex(u, "@"); idx >= 0 {
		u = u[idx+1:]
	}
	if idx := strings.Index(u, "/"); idx >= 0 {
		u = u[:idx]
	}
	if !strings.Contains(u, ":") {
		u = u + ":6379"
	}
	return u
}
