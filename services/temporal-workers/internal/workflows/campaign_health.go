package workflows

import (
	"fmt"

	"github.com/lead/services/temporal-workers/internal/model"
)

const (
	hallucinationThreshold = 5   // max hallucinations in last 50 calls
	providerFailureThreshold = 0.30
	suppressionHitThreshold  = 0.30
)

// EvaluateCampaignHealth evaluates a health snapshot and returns a pause signal
// if any threshold is breached. Pure function used by CampaignHealthWorkflow.
func EvaluateCampaignHealth(snap model.HealthSnapshot) (*model.CampaignPauseSignal, error) {
	if snap.CampaignID == "" {
		return nil, fmt.Errorf("campaign_id required")
	}

	if snap.HallucinationCount > hallucinationThreshold {
		return &model.CampaignPauseSignal{
			CampaignID: snap.CampaignID,
			Reason: fmt.Sprintf(
				"hallucination incidents (%d) in last 50 calls exceed threshold (%d)",
				snap.HallucinationCount, hallucinationThreshold,
			),
		}, nil
	}

	if snap.ProviderFailureRate > providerFailureThreshold {
		return &model.CampaignPauseSignal{
			CampaignID: snap.CampaignID,
			Reason: fmt.Sprintf(
				"provider failure rate %.1f%% exceeds threshold %.1f%%",
				snap.ProviderFailureRate*100, providerFailureThreshold*100,
			),
		}, nil
	}

	if snap.SuppressionHitRate > suppressionHitThreshold {
		return &model.CampaignPauseSignal{
			CampaignID: snap.CampaignID,
			Reason: fmt.Sprintf(
				"suppression hit rate %.1f%% exceeds 30%% threshold",
				snap.SuppressionHitRate*100,
			),
		}, nil
	}

	return nil, nil
}
