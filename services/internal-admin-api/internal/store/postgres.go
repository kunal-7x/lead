package store

import (
	"context"
	"strings"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/internal-admin-api/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	ctx := context.Background()
	kv, err := pgkv.New(ctx, dsn, "svc_internal_admin_api")
	if err != nil {
		return nil, err
	}
	p := &PostgresStore{kv: kv}
	if err := p.seedDefaults(ctx); err != nil {
		kv.Close()
		return nil, err
	}
	return p, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) ListTenants(ctx context.Context) ([]model.Tenant, error) {
	items, err := pgkv.List[model.Tenant](ctx, p.kv, "tenants")
	return items, err
}

func (p *PostgresStore) GetTenant(ctx context.Context, tenantID string) (model.Tenant, bool, error) {
	return pgkv.Get[model.Tenant](ctx, p.kv, "tenants", tenantID)
}

func (p *PostgresStore) SaveTenant(ctx context.Context, tenant model.Tenant) error {
	return p.kv.Put(ctx, "tenants", tenant.ID, tenant)
}

func (p *PostgresStore) ListProviders(ctx context.Context) ([]model.ProviderHealth, error) {
	return pgkv.List[model.ProviderHealth](ctx, p.kv, "providers")
}

func (p *PostgresStore) GetProvider(ctx context.Context, provider string) (model.ProviderHealth, bool, error) {
	return pgkv.Get[model.ProviderHealth](ctx, p.kv, "providers", provider)
}

func (p *PostgresStore) SaveProvider(ctx context.Context, provider model.ProviderHealth) error {
	return p.kv.Put(ctx, "providers", provider.Provider, provider)
}

func (p *PostgresStore) ListFailedJobs(ctx context.Context) ([]model.FailedJob, error) {
	return pgkv.List[model.FailedJob](ctx, p.kv, "failed_jobs")
}

func (p *PostgresStore) SearchLeadByPhone(ctx context.Context, phone string) ([]model.LeadSearchResult, error) {
	leads, err := pgkv.List[model.LeadSearchResult](ctx, p.kv, "leads")
	if err != nil {
		return nil, err
	}
	out := make([]model.LeadSearchResult, 0, len(leads))
	for _, lead := range leads {
		if strings.Contains(lead.Phone, phone) || strings.Contains(phone, lead.Phone) {
			out = append(out, lead)
		}
	}
	return out, nil
}

func (p *PostgresStore) AppendAudit(ctx context.Context, entry model.AuditEntry) error {
	if entry.ID == "" {
		entry.ID = pgkv.NewID("audit")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "audit", entry.ID, entry)
}

func (p *PostgresStore) ListAudit(ctx context.Context, filter model.AuditFilter) ([]model.AuditEntry, error) {
	items, err := pgkv.List[model.AuditEntry](ctx, p.kv, "audit")
	if err != nil {
		return nil, err
	}
	out := make([]model.AuditEntry, 0, len(items))
	for _, item := range items {
		if filter.TenantID != "" && item.TenantID != filter.TenantID {
			continue
		}
		if filter.ActorID != "" && item.ActorID != filter.ActorID {
			continue
		}
		if filter.Action != "" && item.Action != filter.Action {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (p *PostgresStore) ListFeatureFlags(ctx context.Context) ([]model.FeatureFlag, error) {
	return pgkv.List[model.FeatureFlag](ctx, p.kv, "feature_flags")
}

func (p *PostgresStore) SaveFeatureFlag(ctx context.Context, flag model.FeatureFlag) error {
	if flag.UpdatedAt.IsZero() {
		flag.UpdatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "feature_flags", flag.TenantID+":"+flag.Key, flag)
}

func (p *PostgresStore) SaveKillSwitch(ctx context.Context, killSwitch model.KillSwitch) error {
	if killSwitch.UpdatedAt.IsZero() {
		killSwitch.UpdatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "kill_switches", killSwitch.Scope+":"+killSwitch.ScopeID, killSwitch)
}

func (p *PostgresStore) GetKillSwitch(ctx context.Context, scope, scopeID string) (model.KillSwitch, bool, error) {
	return pgkv.Get[model.KillSwitch](ctx, p.kv, "kill_switches", scope+":"+scopeID)
}

func (p *PostgresStore) ListAIQualityItems(ctx context.Context) ([]model.AIQualityItem, error) {
	return pgkv.List[model.AIQualityItem](ctx, p.kv, "ai_quality")
}

func (p *PostgresStore) seedDefaults(ctx context.Context) error {
	inserted, err := p.kv.Insert(ctx, "meta", "seeded", map[string]bool{"seeded": true})
	if err != nil || !inserted {
		return err
	}
	now := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	tenants := []model.Tenant{
		{ID: "tenant-north", Name: "North Star Realty", Status: model.TenantActive, Plan: "growth", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant-west", Name: "Westline Estates", Status: model.TenantActive, Plan: "scale", CreatedAt: now, UpdatedAt: now},
	}
	for _, tenant := range tenants {
		if err := p.kv.Put(ctx, "tenants", tenant.ID, tenant); err != nil {
			return err
		}
	}
	providers := []model.ProviderHealth{
		{Provider: "plivo", Online: true, RollingFailureRate: 0.01, P95LatencyMS: 120, UpdatedAt: now},
		{Provider: "meta_wa", Online: true, RollingFailureRate: 0.03, P95LatencyMS: 180, UpdatedAt: now},
		{Provider: "groq", Online: true, RollingFailureRate: 0.02, P95LatencyMS: 90, UpdatedAt: now},
		{Provider: "openrouter", Online: true, RollingFailureRate: 0.04, P95LatencyMS: 240, UpdatedAt: now},
		{Provider: "sarvam", Online: true, RollingFailureRate: 0.02, P95LatencyMS: 160, UpdatedAt: now},
		{Provider: "elevenlabs", Online: true, RollingFailureRate: 0.05, P95LatencyMS: 260, UpdatedAt: now},
		{Provider: "vllm", Online: false, RollingFailureRate: 1, LastError: "endpoint not configured", UpdatedAt: now},
		{Provider: "indicconformer", Online: true, RollingFailureRate: 0.01, P95LatencyMS: 140, UpdatedAt: now},
	}
	for _, provider := range providers {
		if err := p.kv.Put(ctx, "providers", provider.Provider, provider); err != nil {
			return err
		}
	}
	failedJobs := []model.FailedJob{
		{ID: "dlq-001", Queue: "nats.dlq.calls", Subject: "call.completed", WorkflowID: "wf-991", TenantID: "tenant-north", Error: "billing cap exceeded", Attempts: 3, FailedAt: now.Add(-20 * time.Minute)},
		{ID: "tmp-884", Queue: "temporal.failed", Subject: "campaign-run", WorkflowID: "campaign-77", TenantID: "tenant-west", Error: "provider circuit open", Attempts: 2, FailedAt: now.Add(-46 * time.Minute)},
	}
	for _, job := range failedJobs {
		if err := p.kv.Put(ctx, "failed_jobs", job.ID, job); err != nil {
			return err
		}
	}
	lead := model.LeadSearchResult{
		LeadID:   "lead-9001",
		TenantID: "tenant-north",
		Phone:    "+919876543210",
		Name:     "Amit Verma",
		Status:   "hot",
		Timeline: []model.LeadTimelineEvent{
			{At: now.Add(-2 * time.Hour), Type: "call.started", Detail: "AI qualified 2BHK budget around 85 lakh"},
			{At: now.Add(-90 * time.Minute), Type: "site_visit.booked", Detail: "Booked for Saturday 11:00"},
		},
	}
	if err := p.kv.Put(ctx, "leads", lead.LeadID, lead); err != nil {
		return err
	}
	flags := []model.FeatureFlag{
		{Key: "ai_quality_review", TenantID: "tenant-north", Enabled: true, Description: "Route low-confidence AI sessions to review", UpdatedAt: now},
		{Key: "premium_tts", TenantID: "tenant-west", Enabled: false, Description: "Allow ElevenLabs fallback", UpdatedAt: now},
	}
	for _, flag := range flags {
		if err := p.kv.Put(ctx, "feature_flags", flag.TenantID+":"+flag.Key, flag); err != nil {
			return err
		}
	}
	item := model.AIQualityItem{ID: "qa-101", TenantID: "tenant-north", SessionID: "sess-551", Status: "queued", Reason: "low confidence handoff", CreatedAt: now.Add(-12 * time.Minute)}
	return p.kv.Put(ctx, "ai_quality", item.ID, item)
}
