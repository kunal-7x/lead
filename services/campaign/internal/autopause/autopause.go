package autopause

import (
	"context"
	"fmt"

	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/store"
)

const (
	HallucinationThreshold = 5   // max hallucination incidents in last 50 calls
	ProviderFailureThreshold = 0.3 // 30% provider failure rate
	SuppressionHitThreshold  = 0.3 // 30% suppression hit rate
)

// Evaluator checks health snapshots and auto-pauses campaigns that breach thresholds.
type Evaluator struct {
	store store.Store
}

func New(s store.Store) *Evaluator {
	return &Evaluator{store: s}
}

// EvaluateAndPause checks the latest health snapshot for a campaign and pauses it if
// any auto-pause threshold is breached. Returns the pause reason, or empty string if
// no pause is needed.
func (e *Evaluator) EvaluateAndPause(ctx context.Context, campaignID string) (string, error) {
	campaign, err := e.store.GetCampaign(ctx, campaignID)
	if err != nil {
		return "", fmt.Errorf("get campaign: %w", err)
	}
	if campaign.Status != model.StatusActive {
		return "", nil
	}

	limits, err := e.store.GetLimits(ctx, campaignID)
	if err != nil {
		return "", fmt.Errorf("get limits: %w", err)
	}

	health, err := e.store.GetLatestHealth(ctx, campaignID)
	if err != nil {
		return "", fmt.Errorf("get health: %w", err)
	}

	var reason string

	// Cost cap reached.
	if limits.CostCapINR > 0 && health.CostBurnINR >= limits.CostCapINR {
		reason = fmt.Sprintf("cost cap reached (%.2f / %.2f INR)", health.CostBurnINR, limits.CostCapINR)
	}

	// Suppression hit rate > 30%.
	if reason == "" && health.SuppressionHitRate > SuppressionHitThreshold {
		reason = fmt.Sprintf("suppression hit rate %.1f%% exceeds 30%% threshold — list may be stale", health.SuppressionHitRate*100)
	}

	if reason == "" {
		return "", nil
	}

	if err := e.store.UpdateCampaignStatus(ctx, campaignID, model.StatusPaused, reason); err != nil {
		return "", fmt.Errorf("pause campaign: %w", err)
	}
	return reason, nil
}

// CheckHealthSnapshot evaluates a snapshot directly (used in tests and Temporal workflow).
func CheckHealthSnapshot(snap *model.CampaignHealthSnapshot, limits *model.CampaignLimits) (shouldPause bool, reason string) {
	if limits.CostCapINR > 0 && snap.CostBurnINR >= limits.CostCapINR {
		return true, fmt.Sprintf("cost cap reached (%.2f / %.2f INR)", snap.CostBurnINR, limits.CostCapINR)
	}
	if snap.HallucinationCount > HallucinationThreshold {
		return true, fmt.Sprintf("hallucination incidents (%d) in last 50 calls exceed threshold (%d)", snap.HallucinationCount, HallucinationThreshold)
	}
	if snap.ProviderFailureRate > ProviderFailureThreshold {
		return true, fmt.Sprintf("provider failure rate %.1f%% exceeds threshold %.1f%%", snap.ProviderFailureRate*100, ProviderFailureThreshold*100)
	}
	if snap.SuppressionHitRate > SuppressionHitThreshold {
		return true, fmt.Sprintf("suppression hit rate %.1f%% exceeds 30%% threshold", snap.SuppressionHitRate*100)
	}
	return false, ""
}
