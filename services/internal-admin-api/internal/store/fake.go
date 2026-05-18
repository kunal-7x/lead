package store

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/lead/services/internal-admin-api/internal/model"
)

type Fake struct {
	mu           sync.RWMutex
	tenants      map[string]model.Tenant
	providers    map[string]model.ProviderHealth
	failedJobs   []model.FailedJob
	leads        []model.LeadSearchResult
	audit        []model.AuditEntry
	featureFlags map[string]model.FeatureFlag
	killSwitches map[string]model.KillSwitch
	qualityItems []model.AIQualityItem
}

func NewFake() *Fake {
	now := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	return &Fake{
		tenants: map[string]model.Tenant{
			"tenant-north": {ID: "tenant-north", Name: "North Star Realty", Status: model.TenantActive, Plan: "growth", CreatedAt: now, UpdatedAt: now},
			"tenant-west":  {ID: "tenant-west", Name: "Westline Estates", Status: model.TenantActive, Plan: "scale", CreatedAt: now, UpdatedAt: now},
		},
		providers: map[string]model.ProviderHealth{
			"plivo":          {Provider: "plivo", Online: true, RollingFailureRate: 0.01, P95LatencyMS: 120, UpdatedAt: now},
			"meta_wa":        {Provider: "meta_wa", Online: true, RollingFailureRate: 0.03, P95LatencyMS: 180, UpdatedAt: now},
			"groq":           {Provider: "groq", Online: true, RollingFailureRate: 0.02, P95LatencyMS: 90, UpdatedAt: now},
			"openrouter":     {Provider: "openrouter", Online: true, RollingFailureRate: 0.04, P95LatencyMS: 240, UpdatedAt: now},
			"sarvam":         {Provider: "sarvam", Online: true, RollingFailureRate: 0.02, P95LatencyMS: 160, UpdatedAt: now},
			"elevenlabs":     {Provider: "elevenlabs", Online: true, RollingFailureRate: 0.05, P95LatencyMS: 260, UpdatedAt: now},
			"vllm":           {Provider: "vllm", Online: false, RollingFailureRate: 1, P95LatencyMS: 0, LastError: "endpoint not configured", UpdatedAt: now},
			"indicconformer": {Provider: "indicconformer", Online: true, RollingFailureRate: 0.01, P95LatencyMS: 140, UpdatedAt: now},
		},
		failedJobs: []model.FailedJob{
			{ID: "dlq-001", Queue: "nats.dlq.calls", Subject: "call.completed", WorkflowID: "wf-991", TenantID: "tenant-north", Error: "billing cap exceeded", Attempts: 3, FailedAt: now.Add(-20 * time.Minute)},
			{ID: "tmp-884", Queue: "temporal.failed", Subject: "campaign-run", WorkflowID: "campaign-77", TenantID: "tenant-west", Error: "provider circuit open", Attempts: 2, FailedAt: now.Add(-46 * time.Minute)},
		},
		leads: []model.LeadSearchResult{
			{
				LeadID:   "lead-9001",
				TenantID: "tenant-north",
				Phone:    "+919876543210",
				Name:     "Amit Verma",
				Status:   "hot",
				Timeline: []model.LeadTimelineEvent{
					{At: now.Add(-2 * time.Hour), Type: "call.started", Detail: "AI qualified 2BHK budget around 85 lakh"},
					{At: now.Add(-90 * time.Minute), Type: "site_visit.booked", Detail: "Booked for Saturday 11:00"},
				},
			},
		},
		featureFlags: map[string]model.FeatureFlag{
			"ai_quality_review": {Key: "ai_quality_review", TenantID: "tenant-north", Enabled: true, Description: "Route low-confidence AI sessions to review", UpdatedAt: now},
			"premium_tts":       {Key: "premium_tts", TenantID: "tenant-west", Enabled: false, Description: "Allow ElevenLabs fallback", UpdatedAt: now},
		},
		killSwitches: map[string]model.KillSwitch{},
		qualityItems: []model.AIQualityItem{
			{ID: "qa-101", TenantID: "tenant-north", SessionID: "sess-551", Status: "queued", Reason: "low confidence handoff", CreatedAt: now.Add(-12 * time.Minute)},
		},
	}
}

func (f *Fake) ListTenants(_ context.Context) ([]model.Tenant, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]model.Tenant, 0, len(f.tenants))
	for _, tenant := range f.tenants {
		out = append(out, tenant)
	}
	return out, nil
}

func (f *Fake) GetTenant(_ context.Context, tenantID string) (model.Tenant, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	tenant, ok := f.tenants[tenantID]
	return tenant, ok, nil
}

func (f *Fake) SaveTenant(_ context.Context, tenant model.Tenant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenants[tenant.ID] = tenant
	return nil
}

func (f *Fake) ListProviders(_ context.Context) ([]model.ProviderHealth, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]model.ProviderHealth, 0, len(f.providers))
	for _, provider := range f.providers {
		out = append(out, provider)
	}
	return out, nil
}

func (f *Fake) GetProvider(_ context.Context, provider string) (model.ProviderHealth, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	health, ok := f.providers[provider]
	return health, ok, nil
}

func (f *Fake) SaveProvider(_ context.Context, provider model.ProviderHealth) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers[provider.Provider] = provider
	return nil
}

func (f *Fake) SetProviderInFlight(provider string, inFlight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	health := f.providers[provider]
	health.Provider = provider
	health.Online = true
	health.InFlightCalls = inFlight
	f.providers[provider] = health
}

func (f *Fake) ListFailedJobs(_ context.Context) ([]model.FailedJob, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]model.FailedJob, len(f.failedJobs))
	copy(out, f.failedJobs)
	return out, nil
}

func (f *Fake) SearchLeadByPhone(_ context.Context, phone string) ([]model.LeadSearchResult, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []model.LeadSearchResult
	for _, lead := range f.leads {
		if strings.Contains(lead.Phone, phone) || strings.Contains(phone, lead.Phone) {
			out = append(out, lead)
		}
	}
	return out, nil
}

func (f *Fake) AppendAudit(_ context.Context, entry model.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audit = append(f.audit, entry)
	return nil
}

func (f *Fake) ListAudit(_ context.Context, filter model.AuditFilter) ([]model.AuditEntry, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []model.AuditEntry
	for _, entry := range f.audit {
		if filter.TenantID != "" && entry.TenantID != filter.TenantID {
			continue
		}
		if filter.ActorID != "" && entry.ActorID != filter.ActorID {
			continue
		}
		if filter.Action != "" && entry.Action != filter.Action {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func (f *Fake) ListFeatureFlags(_ context.Context) ([]model.FeatureFlag, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]model.FeatureFlag, 0, len(f.featureFlags))
	for _, flag := range f.featureFlags {
		out = append(out, flag)
	}
	return out, nil
}

func (f *Fake) SaveFeatureFlag(_ context.Context, flag model.FeatureFlag) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.featureFlags[flag.TenantID+":"+flag.Key] = flag
	return nil
}

func (f *Fake) SaveKillSwitch(_ context.Context, killSwitch model.KillSwitch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killSwitches[killSwitch.Scope+":"+killSwitch.ScopeID] = killSwitch
	return nil
}

func (f *Fake) GetKillSwitch(_ context.Context, scope, scopeID string) (model.KillSwitch, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	killSwitch, ok := f.killSwitches[scope+":"+scopeID]
	return killSwitch, ok, nil
}

func (f *Fake) ListAIQualityItems(_ context.Context) ([]model.AIQualityItem, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]model.AIQualityItem, len(f.qualityItems))
	copy(out, f.qualityItems)
	return out, nil
}
