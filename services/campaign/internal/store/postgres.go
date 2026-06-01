package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/campaign/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_campaign")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) CreateCampaign(ctx context.Context, c *model.Campaign) error {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = pgkv.NewID("")
	}
	if c.Status == "" {
		c.Status = model.StatusDraft
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	return p.kv.Put(ctx, "campaigns", c.ID, *c)
}

func (p *PostgresStore) GetCampaign(ctx context.Context, id string) (*model.Campaign, error) {
	c, ok, err := pgkv.Get[model.Campaign](ctx, p.kv, "campaigns", id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("campaign %q not found", id)
	}
	return &c, nil
}

func (p *PostgresStore) ListCampaigns(ctx context.Context, tenantID string) ([]*model.Campaign, error) {
	campaigns, err := pgkv.List[model.Campaign](ctx, p.kv, "campaigns")
	if err != nil {
		return nil, err
	}
	out := make([]*model.Campaign, 0, len(campaigns))
	for _, c := range campaigns {
		if tenantID == "" || c.TenantID == tenantID {
			cp := c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (p *PostgresStore) UpdateCampaignStatus(ctx context.Context, id, status, pauseReason string) error {
	c, err := p.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	c.Status = status
	c.PauseReason = pauseReason
	c.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "campaigns", id, *c)
}

func (p *PostgresStore) UpdateCampaignPinnedVersions(ctx context.Context, id, scriptVersionID, promptVersionID string) error {
	c, err := p.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	c.ScriptVersionID = scriptVersionID
	c.PromptVersionID = promptVersionID
	c.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "campaigns", id, *c)
}

func (p *PostgresStore) UpdateCampaignVoice(ctx context.Context, id, voice string) error {
	c, err := p.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	c.Context.Voice = voice
	c.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "campaigns", id, *c)
}

func (p *PostgresStore) AttachLeads(ctx context.Context, campaignID string, leadIDs []string) error {
	now := time.Now().UTC()
	for _, leadID := range leadIDs {
		lead := model.CampaignLead{CampaignID: campaignID, LeadID: leadID, AttachedAt: now}
		if err := p.kv.Put(ctx, "campaign_leads", campaignID+":"+leadID, lead); err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresStore) ListCampaignLeads(ctx context.Context, campaignID string) ([]*model.CampaignLead, error) {
	leads, err := pgkv.List[model.CampaignLead](ctx, p.kv, "campaign_leads")
	if err != nil {
		return nil, err
	}
	out := make([]*model.CampaignLead, 0, len(leads))
	for _, lead := range leads {
		if lead.CampaignID == campaignID {
			cp := lead
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (p *PostgresStore) SetLimits(ctx context.Context, limits *model.CampaignLimits) error {
	limits.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "campaign_limits", limits.CampaignID, *limits)
}

func (p *PostgresStore) GetLimits(ctx context.Context, campaignID string) (*model.CampaignLimits, error) {
	limits, ok, err := pgkv.Get[model.CampaignLimits](ctx, p.kv, "campaign_limits", campaignID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &model.CampaignLimits{CampaignID: campaignID}, nil
	}
	return &limits, nil
}

func (p *PostgresStore) RecordHealthSnapshot(ctx context.Context, snap *model.CampaignHealthSnapshot) error {
	if snap.ID == "" {
		snap.ID = pgkv.NewID("")
	}
	if snap.SnapshotAt.IsZero() {
		snap.SnapshotAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "health_snapshots", snap.ID, *snap)
}

func (p *PostgresStore) GetLatestHealth(ctx context.Context, campaignID string) (*model.CampaignHealth, error) {
	snaps, err := pgkv.List[model.CampaignHealthSnapshot](ctx, p.kv, "health_snapshots")
	if err != nil {
		return nil, err
	}
	var latest *model.CampaignHealthSnapshot
	for i := range snaps {
		if snaps[i].CampaignID != campaignID {
			continue
		}
		if latest == nil || snaps[i].SnapshotAt.After(latest.SnapshotAt) {
			latest = &snaps[i]
		}
	}
	if latest == nil {
		return &model.CampaignHealth{CampaignID: campaignID}, nil
	}
	return &model.CampaignHealth{
		CampaignID:         latest.CampaignID,
		ConnectRate:        latest.ConnectRate,
		QualifyRate:        latest.QualifyRate,
		CostBurnINR:        latest.CostBurnINR,
		SuppressionHitRate: latest.SuppressionHitRate,
	}, nil
}

func (p *PostgresStore) GetScript(ctx context.Context, campaignID, version string) (*model.CampaignScript, error) {
	script, ok, err := pgkv.Get[model.CampaignScript](ctx, p.kv, "scripts", campaignID+":"+version)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("script %q version %q not found", campaignID, version)
	}
	return &script, nil
}

func (p *PostgresStore) GetPromptVersion(ctx context.Context, campaignID, version string) (*model.CampaignPromptVersion, error) {
	prompt, ok, err := pgkv.Get[model.CampaignPromptVersion](ctx, p.kv, "prompts", campaignID+":"+version)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("prompt %q version %q not found", campaignID, version)
	}
	return &prompt, nil
}

func (p *PostgresStore) GetKbVersion(ctx context.Context, kbVersionID string) (*model.KbVersion, error) {
	version, ok, err := pgkv.Get[model.KbVersion](ctx, p.kv, "kb_versions", kbVersionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("kb version %q not found", kbVersionID)
	}
	return &version, nil
}

func (p *PostgresStore) GetTenantStatus(ctx context.Context, tenantID string) (*model.TenantStatus, error) {
	status, ok, err := pgkv.Get[model.TenantStatus](ctx, p.kv, "tenant_status", tenantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &model.TenantStatus{BillingCapINR: 100000}, nil
	}
	return &status, nil
}
