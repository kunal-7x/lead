// Package routing selects the appropriate telephony provider for a call.
package routing

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/concurrency"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/store"
)

const defaultHealthWindow = 5 * time.Minute

// Router selects a provider based on routing rules, health, and failure rate.
type Router struct {
	store    store.Store
	adapters map[string]adapter.Telephony
	window   time.Duration
	limiter  concurrency.Limiter
}

func New(s store.Store, adapters []adapter.Telephony) *Router {
	return NewWithLimiter(s, adapters, concurrency.New(defaultVobizMaxConcurrent()))
}

// NewWithLimiter creates a Router with an explicit concurrency limiter (useful for tests).
func NewWithLimiter(s store.Store, adapters []adapter.Telephony, lim concurrency.Limiter) *Router {
	r := &Router{
		store:    s,
		adapters: make(map[string]adapter.Telephony, len(adapters)),
		window:   defaultHealthWindow,
		limiter:  lim,
	}
	for _, a := range adapters {
		r.adapters[a.Name()] = a
	}
	return r
}

// defaultVobizMaxConcurrent reads VOBIZ_MAX_CONCURRENT_CALLS (default 3).
func defaultVobizMaxConcurrent() int {
	v := os.Getenv("VOBIZ_MAX_CONCURRENT_CALLS")
	if v == "" {
		return 3
	}
	var n int
	fmt.Sscanf(v, "%d", &n)
	if n <= 0 {
		return 3
	}
	return n
}

func (r *Router) WithWindow(d time.Duration) *Router { r.window = d; return r }

// IncrConcurrency increments the per-tenant concurrency counter.
// Returns concurrency.ErrConcurrencyLimit if the cap is reached.
func (r *Router) IncrConcurrency(ctx context.Context, tenantID string) error {
	return r.limiter.Incr(ctx, tenantID)
}

// DecrConcurrency decrements the per-tenant concurrency counter (call on hangup).
func (r *Router) DecrConcurrency(ctx context.Context, tenantID string) error {
	r.limiter.Decr(ctx, tenantID)
	return nil
}

// DecrConcurrencyForCall decrements the per-tenant concurrency counter for a
// finished call exactly once, regardless of how many terminal signals arrive
// (status=completed/failed/... and the hangup webhook can both fire for one call).
//
// Single-decrement guarantee: it claims a dedicated idempotency key
// "vobiz:concurrency:decr:<providerCallID>" via the store before decrementing.
// The first terminal signal for a call wins; subsequent ones see the key and
// skip. This is independent of the per-delivery webhook event ids.
//
// Tenant resolution: if tenantHint is non-empty it is used directly; otherwise
// the owning tenant is looked up from the call session by providerCallID (inbound
// Vobiz webhooks carry only the CallUUID, not our tenant id).
//
// Fails open: if the tenant cannot be resolved, it does not decrement (so we
// never decrement the wrong tenant), and any store error is non-fatal.
func (r *Router) DecrConcurrencyForCall(ctx context.Context, providerCallID, tenantHint string) error {
	if providerCallID == "" {
		// No call id to dedupe on; fall back to a direct decrement if we at
		// least know the tenant.
		if tenantHint != "" {
			r.limiter.Decr(ctx, tenantHint)
		}
		return nil
	}

	// Claim the one-shot decrement key. SetIdempotencyKey is a no-op insert if
	// the key already exists, so we check-then-set to learn whether we are first.
	decrKey := fmt.Sprintf("vobiz:concurrency:decr:%s", providerCallID)
	_, exists, err := r.store.CheckIdempotencyKey(ctx, decrKey)
	if err != nil {
		return fmt.Errorf("routing: decr idempotency check: %w", err)
	}
	if exists {
		return nil // already decremented for this call
	}

	tenantID := tenantHint
	if tenantID == "" {
		if sess, lerr := r.store.GetCallSessionByProviderCallID(ctx, providerCallID); lerr == nil {
			tenantID = sess.TenantID
		}
	}
	if tenantID == "" {
		// Can't safely attribute the decrement to a tenant; do not guess.
		return nil
	}

	// Mark first so a racing terminal event won't also decrement.
	if err := r.store.SetIdempotencyKey(ctx, decrKey, []byte("decremented")); err != nil {
		return fmt.Errorf("routing: decr idempotency set: %w", err)
	}
	r.limiter.Decr(ctx, tenantID)
	return nil
}

// PlaceCall selects the provider, places the outbound call, and records the
// call session in the store. Demo tenants are isolated to the mock provider.
func (r *Router) PlaceCall(ctx context.Context, req model.CallRequest) (*model.CallSession, error) {
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("call-%d", time.Now().UnixNano())
	}
	if req.MaxDuration <= 0 {
		req.MaxDuration = 300
	}

	selected, err := r.SelectForCall(ctx, req)
	if err != nil {
		return nil, err
	}

	// Enforce per-tenant concurrency cap for Vobiz calls.
	if selected.Name() == "vobiz" && req.TenantID != "" {
		if err := r.limiter.Incr(ctx, req.TenantID); err != nil {
			return nil, fmt.Errorf("routing: %w", err)
		}
	}

	providerCallID, err := selected.PlaceCall(ctx, req)
	if err != nil {
		// Roll back the concurrency counter if the call failed to initiate.
		if selected.Name() == "vobiz" && req.TenantID != "" {
			r.limiter.Decr(ctx, req.TenantID)
		}
		return nil, err
	}

	now := time.Now()
	session := &model.CallSession{
		ID:             req.SessionID,
		TenantID:       req.TenantID,
		ProviderCallID: string(providerCallID),
		ProviderName:   selected.Name(),
		FromNumber:     req.FromNumber,
		ToNumber:       req.ToNumber,
		Status:         "queued",
		StartedAt:      now,
		CreatedAt:      now,
	}
	if err := r.store.StoreCallSession(ctx, session); err != nil {
		return nil, fmt.Errorf("routing: store call session: %w", err)
	}
	return session, nil
}

// SelectForCall enforces demo-mode isolation before falling back to normal
// health/failure routing. Demo tenants and DEMO_MODE=1 can only use "mock".
func (r *Router) SelectForCall(ctx context.Context, req model.CallRequest) (adapter.Telephony, error) {
	if req.Demo || DemoMode(req.TenantID) {
		mock, ok := r.adapters["mock"]
		if !ok {
			return nil, fmt.Errorf("routing: demo tenant requires mock provider")
		}
		if !mock.Healthy(ctx) {
			return nil, fmt.Errorf("routing: demo mock provider unhealthy")
		}
		return mock, nil
	}
	return r.Select(ctx, req.Region)
}

// Select returns the best available adapter for the given region.
// It evaluates routing rules in priority order and skips providers whose
// rolling failure rate exceeds MaxFailRate or whose Healthy() returns false.
func (r *Router) Select(ctx context.Context, region string) (adapter.Telephony, error) {
	rules, err := r.store.ListRoutingRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("routing: list rules: %w", err)
	}

	// Sort rules by priority ascending (lower = preferred).
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})

	for _, rule := range rules {
		// Region filter: empty rule.Region means all regions match.
		if rule.Region != "" && rule.Region != region {
			continue
		}

		a, ok := r.adapters[rule.ProviderID]
		if !ok {
			continue
		}

		// Check adapter health.
		if !a.Healthy(ctx) {
			continue
		}

		// Check rolling failure rate.
		failRate, err := r.store.GetProviderFailureRate(ctx, rule.ProviderID, r.window)
		if err != nil {
			continue
		}
		if failRate > rule.MaxFailRate {
			continue
		}

		return a, nil
	}

	return nil, fmt.Errorf("routing: no healthy provider available for region %q", region)
}

func DemoMode(tenantID string) bool {
	if os.Getenv("DEMO_MODE") == "1" {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(tenantID))
	return normalized == "tenant-demo" ||
		normalized == "demo" ||
		strings.HasPrefix(normalized, "demo-") ||
		strings.HasSuffix(normalized, "-demo")
}
