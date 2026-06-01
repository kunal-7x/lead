package store

import (
	"context"

	"github.com/lead/services/campaign/internal/model"
)

type Store interface {
	// Campaigns
	CreateCampaign(ctx context.Context, c *model.Campaign) error
	GetCampaign(ctx context.Context, id string) (*model.Campaign, error)
	ListCampaigns(ctx context.Context, tenantID string) ([]*model.Campaign, error)
	UpdateCampaignStatus(ctx context.Context, id, status, pauseReason string) error
	UpdateCampaignPinnedVersions(ctx context.Context, id, scriptVersionID, promptVersionID string) error
	// UpdateCampaignVoice sets the campaign's chosen TTS speaker (context.voice).
	UpdateCampaignVoice(ctx context.Context, id, voice string) error

	// Leads
	AttachLeads(ctx context.Context, campaignID string, leadIDs []string) error
	ListCampaignLeads(ctx context.Context, campaignID string) ([]*model.CampaignLead, error)

	// Limits
	SetLimits(ctx context.Context, limits *model.CampaignLimits) error
	GetLimits(ctx context.Context, campaignID string) (*model.CampaignLimits, error)

	// Health snapshots
	RecordHealthSnapshot(ctx context.Context, snap *model.CampaignHealthSnapshot) error
	GetLatestHealth(ctx context.Context, campaignID string) (*model.CampaignHealth, error)

	// Scripts & prompts
	GetScript(ctx context.Context, campaignID, version string) (*model.CampaignScript, error)
	GetPromptVersion(ctx context.Context, campaignID, version string) (*model.CampaignPromptVersion, error)

	// KB version lookup (mocked; in prod calls knowledge service)
	GetKbVersion(ctx context.Context, kbVersionID string) (*model.KbVersion, error)

	// Tenant lookup (mocked; in prod calls tenant-auth service)
	GetTenantStatus(ctx context.Context, tenantID string) (*model.TenantStatus, error)
}
