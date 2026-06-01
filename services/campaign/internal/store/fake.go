package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/campaign/internal/model"
)

type Fake struct {
	mu              sync.RWMutex
	campaigns       map[string]*model.Campaign
	leads           map[string][]*model.CampaignLead
	limits          map[string]*model.CampaignLimits
	healthSnapshots map[string][]*model.CampaignHealthSnapshot
	scripts         map[string]*model.CampaignScript
	prompts         map[string]*model.CampaignPromptVersion
	kbVersions      map[string]*model.KbVersion
	tenants         map[string]*model.TenantStatus
}

func NewFake() *Fake {
	return &Fake{
		campaigns:       make(map[string]*model.Campaign),
		leads:           make(map[string][]*model.CampaignLead),
		limits:          make(map[string]*model.CampaignLimits),
		healthSnapshots: make(map[string][]*model.CampaignHealthSnapshot),
		scripts:         make(map[string]*model.CampaignScript),
		prompts:         make(map[string]*model.CampaignPromptVersion),
		kbVersions:      make(map[string]*model.KbVersion),
		tenants:         make(map[string]*model.TenantStatus),
	}
}

func (f *Fake) CreateCampaign(ctx context.Context, c *model.Campaign) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.Status == "" {
		c.Status = model.StatusDraft
	}
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	cp := *c
	f.campaigns[c.ID] = &cp
	return nil
}

func (f *Fake) GetCampaign(ctx context.Context, id string) (*model.Campaign, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.campaigns[id]
	if !ok {
		return nil, fmt.Errorf("campaign %q not found", id)
	}
	cp := *c
	return &cp, nil
}

func (f *Fake) ListCampaigns(ctx context.Context, tenantID string) ([]*model.Campaign, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*model.Campaign
	for _, c := range f.campaigns {
		if tenantID == "" || c.TenantID == tenantID {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *Fake) UpdateCampaignStatus(ctx context.Context, id, status, pauseReason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return fmt.Errorf("campaign %q not found", id)
	}
	c.Status = status
	c.PauseReason = pauseReason
	c.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) UpdateCampaignPinnedVersions(ctx context.Context, id, scriptVersionID, promptVersionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return fmt.Errorf("campaign %q not found", id)
	}
	c.ScriptVersionID = scriptVersionID
	c.PromptVersionID = promptVersionID
	c.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) UpdateCampaignVoice(ctx context.Context, id, voice string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return fmt.Errorf("campaign %q not found", id)
	}
	c.Context.Voice = voice
	c.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) AttachLeads(ctx context.Context, campaignID string, leadIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, lid := range leadIDs {
		f.leads[campaignID] = append(f.leads[campaignID], &model.CampaignLead{
			CampaignID: campaignID,
			LeadID:     lid,
			AttachedAt: time.Now(),
		})
	}
	return nil
}

func (f *Fake) ListCampaignLeads(ctx context.Context, campaignID string) ([]*model.CampaignLead, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.leads[campaignID], nil
}

func (f *Fake) SetLimits(ctx context.Context, limits *model.CampaignLimits) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	limits.UpdatedAt = time.Now()
	cp := *limits
	f.limits[limits.CampaignID] = &cp
	return nil
}

func (f *Fake) GetLimits(ctx context.Context, campaignID string) (*model.CampaignLimits, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	l, ok := f.limits[campaignID]
	if !ok {
		return &model.CampaignLimits{CampaignID: campaignID}, nil
	}
	cp := *l
	return &cp, nil
}

func (f *Fake) RecordHealthSnapshot(ctx context.Context, snap *model.CampaignHealthSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if snap.ID == "" {
		snap.ID = uuid.New().String()
	}
	if snap.SnapshotAt.IsZero() {
		snap.SnapshotAt = time.Now()
	}
	cp := *snap
	f.healthSnapshots[snap.CampaignID] = append(f.healthSnapshots[snap.CampaignID], &cp)
	return nil
}

func (f *Fake) GetLatestHealth(ctx context.Context, campaignID string) (*model.CampaignHealth, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	snaps := f.healthSnapshots[campaignID]
	if len(snaps) == 0 {
		return &model.CampaignHealth{CampaignID: campaignID}, nil
	}
	s := snaps[len(snaps)-1]
	return &model.CampaignHealth{
		CampaignID:         s.CampaignID,
		ConnectRate:        s.ConnectRate,
		QualifyRate:        s.QualifyRate,
		CostBurnINR:        s.CostBurnINR,
		SuppressionHitRate: s.SuppressionHitRate,
	}, nil
}

func (f *Fake) GetScript(ctx context.Context, campaignID, version string) (*model.CampaignScript, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := campaignID + ":" + version
	s, ok := f.scripts[key]
	if !ok {
		return nil, fmt.Errorf("script %q version %q not found", campaignID, version)
	}
	cp := *s
	return &cp, nil
}

func (f *Fake) GetPromptVersion(ctx context.Context, campaignID, version string) (*model.CampaignPromptVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := campaignID + ":" + version
	p, ok := f.prompts[key]
	if !ok {
		return nil, fmt.Errorf("prompt %q version %q not found", campaignID, version)
	}
	cp := *p
	return &cp, nil
}

func (f *Fake) GetKbVersion(ctx context.Context, kbVersionID string) (*model.KbVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	v, ok := f.kbVersions[kbVersionID]
	if !ok {
		return nil, fmt.Errorf("kb version %q not found", kbVersionID)
	}
	cp := *v
	return &cp, nil
}

func (f *Fake) GetTenantStatus(ctx context.Context, tenantID string) (*model.TenantStatus, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.tenants[tenantID]
	if !ok {
		return &model.TenantStatus{BillingCapINR: 100000, UsedINR: 0}, nil
	}
	cp := *t
	return &cp, nil
}

// SeedKbVersion adds a KB version for testing.
func (f *Fake) SeedKbVersion(v *model.KbVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kbVersions[v.ID] = v
}

// SeedScript adds a script for testing.
func (f *Fake) SeedScript(s *model.CampaignScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[s.CampaignID+":"+s.Version] = s
}

// SeedPrompt adds a prompt version for testing.
func (f *Fake) SeedPrompt(p *model.CampaignPromptVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts[p.CampaignID+":"+p.Version] = p
}

// SeedTenant adds tenant status for testing.
func (f *Fake) SeedTenant(tenantID string, t *model.TenantStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenants[tenantID] = t
}
