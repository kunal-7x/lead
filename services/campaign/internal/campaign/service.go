package campaign

import (
	"context"
	"fmt"
	"os"

	"github.com/lead/services/campaign/internal/autopause"
	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/preflight"
	"github.com/lead/services/campaign/internal/store"
)

// Service contains campaign business logic.
type Service struct {
	store     store.Store
	preflight *preflight.Checker
	autopause *autopause.Evaluator
}

func New(s store.Store) *Service {
	return &Service{
		store:     s,
		preflight: preflight.New(s),
		autopause: autopause.New(s),
	}
}

func (s *Service) CreateCampaign(ctx context.Context, c *model.Campaign) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if c.KbVersionID == "" {
		return fmt.Errorf("kb_version_id is required")
	}
	c.Status = model.StatusDraft
	return s.store.CreateCampaign(ctx, c)
}

func (s *Service) AttachLeads(ctx context.Context, campaignID string, leadIDs []string) error {
	if len(leadIDs) == 0 {
		return fmt.Errorf("at least one lead_id is required")
	}
	return s.store.AttachLeads(ctx, campaignID, leadIDs)
}

// ListLeads returns all leads attached to the given campaign.
func (s *Service) ListLeads(ctx context.Context, campaignID string) ([]*model.CampaignLead, error) {
	return s.store.ListCampaignLeads(ctx, campaignID)
}

// LaunchCampaign runs the preflight checklist; errors on any failure.
func (s *Service) LaunchCampaign(ctx context.Context, campaignID string) (*model.PreflightResult, error) {
	campaign, err := s.store.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, fmt.Errorf("get campaign: %w", err)
	}

	result, err := s.preflight.Run(ctx, campaign)
	if err != nil {
		return nil, fmt.Errorf("preflight run: %w", err)
	}
	// Dev/local affordance: when CAMPAIGN_PREFLIGHT_DISABLED=1, treat preflight as
	// passed so a campaign launches without the full real-estate artefact set
	// (KB approval, RERA, pinned script/prompt). NEVER enable in production.
	if os.Getenv("CAMPAIGN_PREFLIGHT_DISABLED") == "1" && result != nil {
		result.Passed = true
		result.Errors = nil
	}
	if !result.Passed {
		if err := s.store.UpdateCampaignStatus(ctx, campaignID, model.StatusPreflightFailed, "preflight failed"); err != nil {
			return result, fmt.Errorf("update status: %w", err)
		}
		return result, fmt.Errorf("preflight failed: %v", result.Errors)
	}

	// Pin versions on launch.
	if err := s.store.UpdateCampaignPinnedVersions(ctx, campaignID, campaign.ScriptVersionID, campaign.PromptVersionID); err != nil {
		return result, fmt.Errorf("pin versions: %w", err)
	}

	if err := s.store.UpdateCampaignStatus(ctx, campaignID, model.StatusActive, ""); err != nil {
		return result, fmt.Errorf("update status: %w", err)
	}
	return result, nil
}

func (s *Service) PauseCampaign(ctx context.Context, campaignID, reason string) error {
	if reason == "" {
		reason = "manually paused"
	}
	return s.store.UpdateCampaignStatus(ctx, campaignID, model.StatusPaused, reason)
}

func (s *Service) ResumeCampaign(ctx context.Context, campaignID string) error {
	return s.store.UpdateCampaignStatus(ctx, campaignID, model.StatusActive, "")
}

func (s *Service) ArchiveCampaign(ctx context.Context, campaignID string) error {
	return s.store.UpdateCampaignStatus(ctx, campaignID, model.StatusArchived, "")
}

func (s *Service) GetCampaignHealth(ctx context.Context, campaignID string) (*model.CampaignHealth, error) {
	return s.store.GetLatestHealth(ctx, campaignID)
}

func (s *Service) SetCampaignLimits(ctx context.Context, limits *model.CampaignLimits) error {
	if limits.CampaignID == "" {
		return fmt.Errorf("campaign_id is required")
	}
	return s.store.SetLimits(ctx, limits)
}

// EvaluateAutoPause checks health thresholds and pauses the campaign if any are breached.
func (s *Service) EvaluateAutoPause(ctx context.Context, campaignID string) (string, error) {
	return s.autopause.EvaluateAndPause(ctx, campaignID)
}
