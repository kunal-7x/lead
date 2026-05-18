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
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/store"
)

const defaultHealthWindow = 5 * time.Minute

// Router selects a provider based on routing rules, health, and failure rate.
type Router struct {
	store    store.Store
	adapters map[string]adapter.Telephony
	window   time.Duration
}

func New(s store.Store, adapters []adapter.Telephony) *Router {
	r := &Router{
		store:    s,
		adapters: make(map[string]adapter.Telephony, len(adapters)),
		window:   defaultHealthWindow,
	}
	for _, a := range adapters {
		r.adapters[a.Name()] = a
	}
	return r
}

func (r *Router) WithWindow(d time.Duration) *Router { r.window = d; return r }

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
