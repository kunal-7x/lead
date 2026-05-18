package service

import (
	"context"
	"errors"
	"time"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/pricing"
	"github.com/lead/services/billing-meter/internal/store"
)

type Service struct {
	store store.Store
	now   func() time.Time
}

func New(st store.Store) *Service {
	return &Service{store: st, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) RecordUsage(ctx context.Context, usage model.UsageEvent) (model.CostEvent, error) {
	if usage.TenantID == "" || usage.Type == "" {
		return model.CostEvent{}, errors.New("tenant_id and type are required")
	}
	if usage.OccurredAt.IsZero() {
		usage.OccurredAt = s.now()
	}
	cost := s.costFor(usage)
	created, err := s.store.RecordUsageCost(ctx, usage, cost)
	if err != nil {
		return model.CostEvent{}, err
	}
	if created {
		if err := s.evaluateCaps(ctx, usage); err != nil {
			return model.CostEvent{}, err
		}
		if usage.Type == model.UsageCallCompleted {
			if err := s.evaluateRuntimeCutoff(ctx, usage); err != nil {
				return model.CostEvent{}, err
			}
		}
	}
	return cost, nil
}

func (s *Service) GetUsage(ctx context.Context, tenantID, groupBy string) (model.Summary, error) {
	scope := "tenant"
	if groupBy == "campaign" {
		scope = "campaign"
	}
	return s.store.GetSummary(ctx, scope, tenantID)
}

func (s *Service) GetCostSummary(ctx context.Context, scopeType, scopeID string) (model.Summary, error) {
	return s.store.GetSummary(ctx, scopeType, scopeID)
}

func (s *Service) SetCaps(ctx context.Context, caps model.Caps) error {
	return s.store.SetCaps(ctx, caps)
}

func (s *Service) GetBalance(ctx context.Context, tenantID string) (float64, error) {
	return s.store.GetBalance(ctx, tenantID)
}

func (s *Service) Charge(ctx context.Context, tenantID string, amount float64, reason string) (model.CreditLedgerEntry, error) {
	return s.store.AddCredit(ctx, model.CreditLedgerEntry{TenantID: tenantID, AmountINR: -amount, Reason: reason, CreatedAt: s.now()})
}

func (s *Service) AddCredit(ctx context.Context, tenantID string, amount float64, reason string) (model.CreditLedgerEntry, error) {
	return s.store.AddCredit(ctx, model.CreditLedgerEntry{TenantID: tenantID, AmountINR: amount, Reason: reason, CreatedAt: s.now()})
}

func (s *Service) Store() store.Store {
	return s.store
}

func (s *Service) costFor(usage model.UsageEvent) model.CostEvent {
	cost := model.CostEvent{
		TenantID:   usage.TenantID,
		CampaignID: usage.CampaignID,
		Type:       usage.Type,
		CreatedAt:  s.now(),
	}
	switch usage.Type {
	case model.UsageCallCompleted:
		billedSeconds, total := pricing.CallCostINR(usage.Quantity)
		cost.Quantity = billedSeconds
		cost.Unit = "second"
		cost.UnitCostINR = total / float64(maxInt64(1, billedSeconds))
		cost.TotalINR = total
	case model.UsageWhatsApp:
		unit := pricing.WhatsAppCostINR(usage.Category)
		qty := maxInt64(1, usage.Quantity)
		cost.Quantity = qty
		cost.Unit = "message"
		cost.UnitCostINR = unit
		cost.TotalINR = float64(qty) * unit
	case model.UsageTTS:
		qty := maxInt64(1, usage.Quantity)
		cost.Quantity = qty
		cost.Unit = "character"
		cost.UnitCostINR = pricing.TTSPerCharacterINR
		cost.TotalINR = float64(qty) * pricing.TTSPerCharacterINR
	case model.UsageSTT:
		qty := maxInt64(1, usage.Quantity)
		cost.Quantity = qty
		cost.Unit = "second"
		cost.UnitCostINR = pricing.STTPerSecondINR
		cost.TotalINR = float64(qty) * pricing.STTPerSecondINR
	case model.UsageLLM:
		qty := maxInt64(1, usage.Quantity)
		cost.Quantity = qty
		cost.Unit = "token"
		cost.UnitCostINR = pricing.LLMPerTokenINR
		cost.TotalINR = float64(qty) * pricing.LLMPerTokenINR
	}
	return cost
}

func (s *Service) evaluateCaps(ctx context.Context, usage model.UsageEvent) error {
	tenantCaps, _ := s.store.GetCaps(ctx, usage.TenantID, "")
	if tenantCaps.MonthlyINR > 0 {
		summary, _ := s.store.GetSummary(ctx, "tenant", usage.TenantID)
		if err := s.emitCapSignals(ctx, usage.TenantID, "", tenantCaps.MonthlyINR, summary.TotalCostINR); err != nil {
			return err
		}
	}
	if usage.CampaignID != "" {
		campaignCaps, _ := s.store.GetCaps(ctx, usage.TenantID, usage.CampaignID)
		if campaignCaps.MonthlyINR > 0 {
			summary, _ := s.store.GetSummary(ctx, "campaign", usage.CampaignID)
			if err := s.emitCapSignals(ctx, usage.TenantID, usage.CampaignID, campaignCaps.MonthlyINR, summary.TotalCostINR); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) emitCapSignals(ctx context.Context, tenantID, campaignID string, capINR, usedINR float64) error {
	if usedINR >= capINR*0.80 {
		if _, err := s.store.AddSignal(ctx, model.Signal{
			TenantID:   tenantID,
			CampaignID: campaignID,
			Type:       "billing.cap.warning",
			Payload:    map[string]any{"used_inr": usedINR, "cap_inr": capINR},
			CreatedAt:  s.now(),
		}); err != nil {
			return err
		}
	}
	if usedINR >= capINR {
		if _, err := s.store.AddSignal(ctx, model.Signal{
			TenantID:   tenantID,
			CampaignID: campaignID,
			Type:       "tenant.cap.reached",
			Payload:    map[string]any{"used_inr": usedINR, "cap_inr": capINR},
			CreatedAt:  s.now(),
		}); err != nil {
			return err
		}
		_, err := s.store.AddSignal(ctx, model.Signal{
			TenantID:   tenantID,
			CampaignID: campaignID,
			Type:       "campaign.auto_pause.requested",
			Payload:    map[string]any{"used_inr": usedINR, "cap_inr": capINR},
			CreatedAt:  s.now(),
		})
		return err
	}
	return nil
}

func (s *Service) evaluateRuntimeCutoff(ctx context.Context, usage model.UsageEvent) error {
	caps, _ := s.store.GetCaps(ctx, usage.TenantID, usage.CampaignID)
	if caps.MaxCallSeconds > 0 && usage.Quantity > caps.MaxCallSeconds {
		_, err := s.store.AddSignal(ctx, model.Signal{
			TenantID:   usage.TenantID,
			CampaignID: usage.CampaignID,
			Type:       "call.cutoff",
			Payload:    map[string]any{"max_call_seconds": caps.MaxCallSeconds, "actual_seconds": usage.Quantity},
			CreatedAt:  s.now(),
		})
		return err
	}
	return nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
