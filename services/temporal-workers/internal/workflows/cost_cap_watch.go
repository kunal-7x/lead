package workflows

import (
	"fmt"

	"github.com/lead/services/temporal-workers/internal/model"
)

// EvaluateCostCap evaluates billing events and returns pause signals for
// campaigns over their cost cap. Pure function used by CostCapWatchWorkflow.
func EvaluateCostCap(input model.CostCapWatchInput, events []model.BillingMeterEvent) ([]model.CampaignPauseSignal, error) {
	if input.TenantID == "" {
		return nil, fmt.Errorf("tenant_id required")
	}

	var signals []model.CampaignPauseSignal
	seen := make(map[string]bool)

	for _, ev := range events {
		if ev.TenantID != input.TenantID {
			continue
		}
		if seen[ev.CampaignID] {
			continue
		}

		cap := ev.CapINR
		if cap <= 0 {
			cap = input.CapINR
		}
		if cap > 0 && ev.CostBurnINR >= cap {
			signals = append(signals, model.CampaignPauseSignal{
				CampaignID: ev.CampaignID,
				Reason:     fmt.Sprintf("cost cap reached (%.2f / %.2f INR)", ev.CostBurnINR, cap),
			})
			seen[ev.CampaignID] = true
		}
	}

	return signals, nil
}
