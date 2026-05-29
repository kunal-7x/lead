package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// Fake is a thread-safe in-memory store for testing.
type Fake struct {
	mu              sync.Mutex
	callSessions    map[string]*model.CallSession
	providerEvents  []*model.CallProviderEvent
	recordings      map[string]*model.CallRecording // providerCallID → recording
	providers       []*model.Provider
	providerCreds   map[string]*model.ProviderCredential
	healthChecks    []*model.ProviderHealthCheck
	providerFails   []*model.ProviderFailure
	routingRules    []*model.ProviderRoutingRule
	webhookEvents   map[string]*model.WebhookEvent
	idempotencyKeys map[string]*model.IdempotencyKey
}

func NewFake() *Fake {
	f := &Fake{
		callSessions:    make(map[string]*model.CallSession),
		recordings:      make(map[string]*model.CallRecording),
		providerCreds:   make(map[string]*model.ProviderCredential),
		webhookEvents:   make(map[string]*model.WebhookEvent),
		idempotencyKeys: make(map[string]*model.IdempotencyKey),
	}
	// seed default providers
	f.providers = []*model.Provider{
		{ID: "plivo", Name: "plivo", Enabled: true, Priority: 1},
		{ID: "exotel", Name: "exotel", Enabled: true, Priority: 2},
		{ID: "twilio", Name: "twilio", Enabled: true, Priority: 3},
		{ID: "mock", Name: "mock", Enabled: true, Priority: 4},
	}
	f.routingRules = []*model.ProviderRoutingRule{
		{ID: "rule-plivo", ProviderID: "plivo", Priority: 1, MaxFailRate: 0.3},
		{ID: "rule-exotel", ProviderID: "exotel", Priority: 2, MaxFailRate: 0.3},
		{ID: "rule-mock", ProviderID: "mock", Priority: 4, MaxFailRate: 1.0},
	}
	return f
}

func (f *Fake) StoreCallSession(_ context.Context, s *model.CallSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.callSessions[s.ID] = &cp
	return nil
}

func (f *Fake) GetCallSession(_ context.Context, id string) (*model.CallSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.callSessions[id]
	if !ok {
		return nil, fmt.Errorf("store: call session %s not found", id)
	}
	cp := *s
	return &cp, nil
}

func (f *Fake) GetCallSessionByProviderCallID(_ context.Context, providerCallID string) (*model.CallSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.callSessions {
		if s.ProviderCallID == providerCallID {
			cp := *s
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("store: call session for provider_call_id %s not found", providerCallID)
}

func (f *Fake) StoreProviderEvent(_ context.Context, e *model.CallProviderEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *e
	f.providerEvents = append(f.providerEvents, &cp)
	return nil
}

func (f *Fake) StoreRecording(_ context.Context, r *model.CallRecording) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.recordings[r.ProviderCallID] = &cp
	return nil
}

func (f *Fake) GetRecording(_ context.Context, providerCallID string) (*model.CallRecording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recordings[providerCallID]
	if !ok {
		return nil, fmt.Errorf("store: recording for call %s not found", providerCallID)
	}
	cp := *r
	return &cp, nil
}

func (f *Fake) ListProviders(_ context.Context) ([]*model.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*model.Provider, len(f.providers))
	for i, p := range f.providers {
		cp := *p
		out[i] = &cp
	}
	return out, nil
}

func (f *Fake) GetProviderCredentials(_ context.Context, providerID string) (*model.ProviderCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.providerCreds[providerID]
	if !ok {
		return &model.ProviderCredential{ProviderID: providerID}, nil
	}
	cp := *c
	return &cp, nil
}

func (f *Fake) SetProviderCredentials(providerID, authID, authToken string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providerCreds[providerID] = &model.ProviderCredential{
		ProviderID: providerID,
		AuthID:     authID,
		AuthToken:  authToken,
	}
}

func (f *Fake) StoreHealthCheck(_ context.Context, h *model.ProviderHealthCheck) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *h
	f.healthChecks = append(f.healthChecks, &cp)
	return nil
}

func (f *Fake) StoreProviderFailure(_ context.Context, fail *model.ProviderFailure) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *fail
	f.providerFails = append(f.providerFails, &cp)
	return nil
}

func (f *Fake) GetProviderFailureRate(_ context.Context, providerID string, window time.Duration) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cutoff := time.Now().Add(-window)
	var total, fails int
	for _, hc := range f.healthChecks {
		if hc.ProviderID == providerID && hc.CheckedAt.After(cutoff) {
			total++
			if !hc.Healthy {
				fails++
			}
		}
	}
	for _, pf := range f.providerFails {
		if pf.ProviderID == providerID && pf.OccurredAt.After(cutoff) {
			total++
			fails++
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(fails) / float64(total), nil
}

func (f *Fake) ListRoutingRules(_ context.Context) ([]*model.ProviderRoutingRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*model.ProviderRoutingRule, len(f.routingRules))
	for i, r := range f.routingRules {
		cp := *r
		out[i] = &cp
	}
	return out, nil
}

func (f *Fake) StoreWebhookEvent(_ context.Context, e *model.WebhookEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *e
	f.webhookEvents[e.ID] = &cp
	return nil
}

func (f *Fake) CheckIdempotencyKey(_ context.Context, key string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.idempotencyKeys[key]
	if !ok {
		return nil, false, nil
	}
	return k.Result, true, nil
}

func (f *Fake) SetIdempotencyKey(_ context.Context, key string, result []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.idempotencyKeys[key]; exists {
		return nil // already set; idempotent
	}
	f.idempotencyKeys[key] = &model.IdempotencyKey{
		Key:       key,
		Result:    result,
		CreatedAt: time.Now(),
	}
	return nil
}

// IdempotencyKeyCount returns the number of stored idempotency keys (for testing).
func (f *Fake) IdempotencyKeyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.idempotencyKeys)
}

// WebhookEventCount returns the number of stored webhook events.
func (f *Fake) WebhookEventCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.webhookEvents)
}

// RecordingCount returns the number of stored recordings (for testing).
func (f *Fake) RecordingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recordings)
}

// AddRoutingRule replaces the routing rules with the provided rule set (for routing tests).
// Call before building the router to control which rules are active.
func (f *Fake) AddRoutingRule(r model.ProviderRoutingRule) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := r
	f.routingRules = append(f.routingRules, &cp)
}

// UpsertRoutingRule inserts or replaces a routing rule by ID.
func (f *Fake) UpsertRoutingRule(_ context.Context, rule *model.ProviderRoutingRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.routingRules {
		if r.ID == rule.ID {
			cp := *rule
			f.routingRules[i] = &cp
			return nil
		}
	}
	cp := *rule
	f.routingRules = append(f.routingRules, &cp)
	return nil
}

// ResetRoutingRules clears all routing rules.
func (f *Fake) ResetRoutingRules() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routingRules = nil
}

// AddProviderFailures adds synthetic failures for a provider (for routing tests).
func (f *Fake) AddProviderFailures(providerID string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 0; i < count; i++ {
		f.providerFails = append(f.providerFails, &model.ProviderFailure{
			ID:         fmt.Sprintf("fail-%s-%d", providerID, i),
			ProviderID: providerID,
			Reason:     "simulated failure",
			OccurredAt: time.Now(),
		})
		f.healthChecks = append(f.healthChecks, &model.ProviderHealthCheck{
			ID:         fmt.Sprintf("hc-%s-%d", providerID, i),
			ProviderID: providerID,
			Healthy:    false,
			CheckedAt:  time.Now(),
		})
	}
}
