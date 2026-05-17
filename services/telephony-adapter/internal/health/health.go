// Package health monitors provider health and computes rolling failure rates.
package health

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/store"
)

const (
	defaultWindow  = 5 * time.Minute
	defaultInterval = 30 * time.Second
)

// Monitor runs background health checks for all registered providers.
type Monitor struct {
	store    store.Store
	adapters map[string]adapter.Telephony
	interval time.Duration
	window   time.Duration
}

func New(s store.Store, adapters []adapter.Telephony) *Monitor {
	m := &Monitor{
		store:    s,
		adapters: make(map[string]adapter.Telephony, len(adapters)),
		interval: defaultInterval,
		window:   defaultWindow,
	}
	for _, a := range adapters {
		m.adapters[a.Name()] = a
	}
	return m
}

func (m *Monitor) WithInterval(d time.Duration) *Monitor { m.interval = d; return m }
func (m *Monitor) WithWindow(d time.Duration) *Monitor   { m.window = d; return m }

// Run starts the background health check loop. It blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	tick := time.NewTicker(m.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.checkAll(ctx)
		}
	}
}

func (m *Monitor) checkAll(ctx context.Context) {
	for name, a := range m.adapters {
		healthy := a.Healthy(ctx)
		hc := &model.ProviderHealthCheck{
			ID:         fmt.Sprintf("hc-%s-%d", name, time.Now().UnixNano()),
			ProviderID: name,
			Healthy:    healthy,
			CheckedAt:  time.Now(),
		}
		_ = m.store.StoreHealthCheck(ctx, hc)
	}
}

// CheckOnce performs a single health check pass (useful for tests).
func (m *Monitor) CheckOnce(ctx context.Context) {
	m.checkAll(ctx)
}

// FailureRate returns the rolling failure rate for a provider.
func (m *Monitor) FailureRate(ctx context.Context, providerID string) (float64, error) {
	return m.store.GetProviderFailureRate(ctx, providerID, m.window)
}
