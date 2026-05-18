package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lead/services/billing-meter/internal/model"
)

type Fake struct {
	mu              sync.Mutex
	seq             int
	idempotency     map[string]bool
	costEvents      []*model.CostEvent
	tenantSummary   map[string]*model.Summary
	campaignSummary map[string]*model.Summary
	caps            map[string]model.Caps
	credits         map[string]float64
	signals         []*model.Signal
}

func NewFake() *Fake {
	return &Fake{
		idempotency:     make(map[string]bool),
		tenantSummary:   make(map[string]*model.Summary),
		campaignSummary: make(map[string]*model.Summary),
		caps:            make(map[string]model.Caps),
		credits:         make(map[string]float64),
	}
}

func (f *Fake) RecordUsageCost(_ context.Context, usage model.UsageEvent, cost model.CostEvent) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if usage.IdempotencyKey != "" {
		if f.idempotency[usage.IdempotencyKey] {
			return false, nil
		}
		f.idempotency[usage.IdempotencyKey] = true
	}
	if usage.ID == "" {
		usage.ID = f.nextID("usage")
	}
	if cost.ID == "" {
		cost.ID = f.nextID("cost")
	}
	if cost.CreatedAt.IsZero() {
		cost.CreatedAt = time.Now().UTC()
	}
	cost.UsageEventID = usage.ID
	cp := cost
	f.costEvents = append(f.costEvents, &cp)
	f.addSummary(f.tenantSummary, usage.TenantID, "tenant", cost)
	if usage.CampaignID != "" {
		f.addSummary(f.campaignSummary, usage.CampaignID, "campaign", cost)
	}
	return true, nil
}

func (f *Fake) GetSummary(_ context.Context, scopeType, scopeID string) (model.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var src map[string]*model.Summary
	if scopeType == "campaign" {
		src = f.campaignSummary
	} else {
		src = f.tenantSummary
	}
	summary, ok := src[scopeID]
	if !ok {
		return model.Summary{ScopeID: scopeID, ScopeType: scopeType}, nil
	}
	return *summary, nil
}

func (f *Fake) SetCaps(_ context.Context, caps model.Caps) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.caps[capKey(caps.TenantID, caps.CampaignID)] = caps
	return nil
}

func (f *Fake) GetCaps(_ context.Context, tenantID, campaignID string) (model.Caps, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.caps[capKey(tenantID, campaignID)], nil
}

func (f *Fake) AddCredit(_ context.Context, entry model.CreditLedgerEntry) (model.CreditLedgerEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry.ID == "" {
		entry.ID = f.nextID("credit")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	f.credits[entry.TenantID] += entry.AmountINR
	return entry, nil
}

func (f *Fake) GetBalance(_ context.Context, tenantID string) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.credits[tenantID], nil
}

func (f *Fake) AddSignal(_ context.Context, signal model.Signal) (model.Signal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if signal.ID == "" {
		signal.ID = f.nextID("signal")
	}
	if signal.CreatedAt.IsZero() {
		signal.CreatedAt = time.Now().UTC()
	}
	cp := signal
	f.signals = append(f.signals, &cp)
	return signal, nil
}

func (f *Fake) ListSignals(_ context.Context, tenantID string) ([]model.Signal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Signal, 0, len(f.signals))
	for _, signal := range f.signals {
		if tenantID == "" || signal.TenantID == tenantID {
			out = append(out, *signal)
		}
	}
	return out, nil
}

func (f *Fake) ListCostEvents(_ context.Context, tenantID string) ([]model.CostEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.CostEvent, 0, len(f.costEvents))
	for _, event := range f.costEvents {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, *event)
		}
	}
	return out, nil
}

func (f *Fake) addSummary(dst map[string]*model.Summary, id, scopeType string, cost model.CostEvent) {
	summary := dst[id]
	if summary == nil {
		summary = &model.Summary{ScopeID: id, ScopeType: scopeType}
		dst[id] = summary
	}
	summary.UsageCount++
	summary.TotalCostINR += cost.TotalINR
}

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%06d", prefix, f.seq)
}

func capKey(tenantID, campaignID string) string {
	if campaignID != "" {
		return "campaign:" + campaignID
	}
	return "tenant:" + tenantID
}
