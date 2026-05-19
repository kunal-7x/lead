package store

import (
	"context"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/billing-meter/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_billing_meter")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) RecordUsageCost(ctx context.Context, usage model.UsageEvent, cost model.CostEvent) (bool, error) {
	if usage.IdempotencyKey != "" {
		inserted, err := p.kv.Insert(ctx, "idempotency_keys", usage.IdempotencyKey, map[string]string{"key": usage.IdempotencyKey})
		if err != nil || !inserted {
			return inserted, err
		}
	}
	if usage.ID == "" {
		usage.ID = pgkv.NewID("usage")
	}
	if usage.OccurredAt.IsZero() {
		usage.OccurredAt = time.Now().UTC()
	}
	if cost.ID == "" {
		cost.ID = pgkv.NewID("cost")
	}
	if cost.CreatedAt.IsZero() {
		cost.CreatedAt = time.Now().UTC()
	}
	cost.UsageEventID = usage.ID
	if err := p.kv.Put(ctx, "usage_events", usage.ID, usage); err != nil {
		return false, err
	}
	if err := p.kv.Put(ctx, "cost_events", cost.ID, cost); err != nil {
		return false, err
	}
	return true, nil
}

func (p *PostgresStore) GetSummary(ctx context.Context, scopeType, scopeID string) (model.Summary, error) {
	costs, err := pgkv.List[model.CostEvent](ctx, p.kv, "cost_events")
	if err != nil {
		return model.Summary{}, err
	}
	summary := model.Summary{ScopeID: scopeID, ScopeType: scopeType}
	for _, cost := range costs {
		if scopeType == "campaign" {
			if cost.CampaignID != scopeID {
				continue
			}
		} else if cost.TenantID != scopeID {
			continue
		}
		summary.UsageCount++
		summary.TotalCostINR += cost.TotalINR
	}
	return summary, nil
}

func (p *PostgresStore) SetCaps(ctx context.Context, caps model.Caps) error {
	return p.kv.Put(ctx, "caps", capKey(caps.TenantID, caps.CampaignID), caps)
}

func (p *PostgresStore) GetCaps(ctx context.Context, tenantID, campaignID string) (model.Caps, error) {
	caps, ok, err := pgkv.Get[model.Caps](ctx, p.kv, "caps", capKey(tenantID, campaignID))
	if err != nil || ok {
		return caps, err
	}
	return model.Caps{}, nil
}

func (p *PostgresStore) AddCredit(ctx context.Context, entry model.CreditLedgerEntry) (model.CreditLedgerEntry, error) {
	if entry.ID == "" {
		entry.ID = pgkv.NewID("credit")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	return entry, p.kv.Put(ctx, "credits", entry.ID, entry)
}

func (p *PostgresStore) GetBalance(ctx context.Context, tenantID string) (float64, error) {
	credits, err := pgkv.List[model.CreditLedgerEntry](ctx, p.kv, "credits")
	if err != nil {
		return 0, err
	}
	var balance float64
	for _, credit := range credits {
		if credit.TenantID == tenantID {
			balance += credit.AmountINR
		}
	}
	return balance, nil
}

func (p *PostgresStore) AddSignal(ctx context.Context, signal model.Signal) (model.Signal, error) {
	if signal.ID == "" {
		signal.ID = pgkv.NewID("signal")
	}
	if signal.CreatedAt.IsZero() {
		signal.CreatedAt = time.Now().UTC()
	}
	return signal, p.kv.Put(ctx, "signals", signal.ID, signal)
}

func (p *PostgresStore) ListSignals(ctx context.Context, tenantID string) ([]model.Signal, error) {
	items, err := pgkv.List[model.Signal](ctx, p.kv, "signals")
	if err != nil {
		return nil, err
	}
	out := make([]model.Signal, 0, len(items))
	for _, item := range items {
		if tenantID == "" || item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *PostgresStore) ListCostEvents(ctx context.Context, tenantID string) ([]model.CostEvent, error) {
	items, err := pgkv.List[model.CostEvent](ctx, p.kv, "cost_events")
	if err != nil {
		return nil, err
	}
	out := make([]model.CostEvent, 0, len(items))
	for _, item := range items {
		if tenantID == "" || item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}
