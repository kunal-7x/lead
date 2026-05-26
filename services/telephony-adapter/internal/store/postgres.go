package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/telephony-adapter/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	ctx := context.Background()
	kv, err := pgkv.New(ctx, dsn, "svc_telephony_adapter")
	if err != nil {
		return nil, err
	}
	p := &PostgresStore{kv: kv}
	if err := p.seedDefaults(ctx); err != nil {
		kv.Close()
		return nil, err
	}
	return p, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) StoreCallSession(ctx context.Context, s *model.CallSession) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if err := p.kv.Put(ctx, "call_sessions", s.ID, *s); err != nil {
		return err
	}
	// Secondary index so inbound webhooks (which only carry the provider call id)
	// can resolve the owning session/tenant.
	if s.ProviderCallID != "" {
		if err := p.kv.Put(ctx, "call_sessions_by_provider_call", s.ProviderCallID, *s); err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresStore) GetCallSessionByProviderCallID(ctx context.Context, providerCallID string) (*model.CallSession, error) {
	s, ok, err := pgkv.Get[model.CallSession](ctx, p.kv, "call_sessions_by_provider_call", providerCallID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("store: call session for provider_call_id %s not found", providerCallID)
	}
	return &s, nil
}

func (p *PostgresStore) GetCallSession(ctx context.Context, sessionID string) (*model.CallSession, error) {
	s, ok, err := pgkv.Get[model.CallSession](ctx, p.kv, "call_sessions", sessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("store: call session %s not found", sessionID)
	}
	return &s, nil
}

func (p *PostgresStore) StoreProviderEvent(ctx context.Context, e *model.CallProviderEvent) error {
	if e.ID == "" {
		e.ID = pgkv.NewID("")
	}
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "provider_events", e.ID, *e)
}

func (p *PostgresStore) StoreRecording(ctx context.Context, r *model.CallRecording) error {
	if r.ID == "" {
		r.ID = pgkv.NewID("")
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if err := p.kv.Put(ctx, "recordings", r.ID, *r); err != nil {
		return err
	}
	return p.kv.Put(ctx, "recordings_by_provider_call", r.ProviderCallID, *r)
}

func (p *PostgresStore) GetRecording(ctx context.Context, providerCallID string) (*model.CallRecording, error) {
	r, ok, err := pgkv.Get[model.CallRecording](ctx, p.kv, "recordings_by_provider_call", providerCallID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("store: recording for call %s not found", providerCallID)
	}
	return &r, nil
}

func (p *PostgresStore) ListProviders(ctx context.Context) ([]*model.Provider, error) {
	providers, err := pgkv.List[model.Provider](ctx, p.kv, "providers")
	if err != nil {
		return nil, err
	}
	out := make([]*model.Provider, 0, len(providers))
	for _, provider := range providers {
		cp := provider
		out = append(out, &cp)
	}
	return out, nil
}

func (p *PostgresStore) GetProviderCredentials(ctx context.Context, providerID string) (*model.ProviderCredential, error) {
	cred, ok, err := pgkv.Get[model.ProviderCredential](ctx, p.kv, "provider_credentials", providerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &model.ProviderCredential{ProviderID: providerID}, nil
	}
	return &cred, nil
}

func (p *PostgresStore) StoreHealthCheck(ctx context.Context, h *model.ProviderHealthCheck) error {
	if h.ID == "" {
		h.ID = pgkv.NewID("")
	}
	if h.CheckedAt.IsZero() {
		h.CheckedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "health_checks", h.ID, *h)
}

func (p *PostgresStore) StoreProviderFailure(ctx context.Context, f *model.ProviderFailure) error {
	if f.ID == "" {
		f.ID = pgkv.NewID("")
	}
	if f.OccurredAt.IsZero() {
		f.OccurredAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "provider_failures", f.ID, *f)
}

func (p *PostgresStore) GetProviderFailureRate(ctx context.Context, providerID string, window time.Duration) (float64, error) {
	cutoff := time.Now().UTC().Add(-window)
	var total, failed int
	checks, err := pgkv.List[model.ProviderHealthCheck](ctx, p.kv, "health_checks")
	if err != nil {
		return 0, err
	}
	for _, check := range checks {
		if check.ProviderID == providerID && check.CheckedAt.After(cutoff) {
			total++
			if !check.Healthy {
				failed++
			}
		}
	}
	failures, err := pgkv.List[model.ProviderFailure](ctx, p.kv, "provider_failures")
	if err != nil {
		return 0, err
	}
	for _, failure := range failures {
		if failure.ProviderID == providerID && failure.OccurredAt.After(cutoff) {
			total++
			failed++
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(failed) / float64(total), nil
}

func (p *PostgresStore) ListRoutingRules(ctx context.Context) ([]*model.ProviderRoutingRule, error) {
	rules, err := pgkv.List[model.ProviderRoutingRule](ctx, p.kv, "routing_rules")
	if err != nil {
		return nil, err
	}
	out := make([]*model.ProviderRoutingRule, 0, len(rules))
	for _, rule := range rules {
		cp := rule
		out = append(out, &cp)
	}
	return out, nil
}

func (p *PostgresStore) StoreWebhookEvent(ctx context.Context, e *model.WebhookEvent) error {
	if e.ID == "" {
		e.ID = pgkv.NewID("")
	}
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "webhook_events", e.ID, *e)
}

func (p *PostgresStore) CheckIdempotencyKey(ctx context.Context, key string) ([]byte, bool, error) {
	item, ok, err := pgkv.Get[model.IdempotencyKey](ctx, p.kv, "idempotency_keys", key)
	if err != nil || !ok {
		return nil, ok, err
	}
	return item.Result, true, nil
}

func (p *PostgresStore) SetIdempotencyKey(ctx context.Context, key string, result []byte) error {
	_, err := p.kv.Insert(ctx, "idempotency_keys", key, model.IdempotencyKey{
		Key:       key,
		Result:    append([]byte(nil), result...),
		CreatedAt: time.Now().UTC(),
	})
	return err
}

func (p *PostgresStore) seedDefaults(ctx context.Context) error {
	providers := []model.Provider{
		{ID: "plivo", Name: "plivo", Enabled: true, Priority: 1},
		{ID: "exotel", Name: "exotel", Enabled: true, Priority: 2},
		{ID: "twilio", Name: "twilio", Enabled: true, Priority: 3},
		{ID: "mock", Name: "mock", Enabled: true, Priority: 4},
	}
	for _, provider := range providers {
		if _, err := p.kv.Insert(ctx, "providers", provider.ID, provider); err != nil {
			return err
		}
	}
	rules := []model.ProviderRoutingRule{
		{ID: "rule-plivo", ProviderID: "plivo", Priority: 1, MaxFailRate: 0.3},
		{ID: "rule-exotel", ProviderID: "exotel", Priority: 2, MaxFailRate: 0.3},
		{ID: "rule-mock", ProviderID: "mock", Priority: 4, MaxFailRate: 1.0},
	}
	for _, rule := range rules {
		if _, err := p.kv.Insert(ctx, "routing_rules", rule.ID, rule); err != nil {
			return err
		}
	}
	return nil
}
