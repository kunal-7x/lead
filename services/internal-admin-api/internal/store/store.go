package store

import (
	"context"

	"github.com/lead/services/internal-admin-api/internal/model"
)

type Store interface {
	ListTenants(ctx context.Context) ([]model.Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (model.Tenant, bool, error)
	SaveTenant(ctx context.Context, tenant model.Tenant) error

	ListProviders(ctx context.Context) ([]model.ProviderHealth, error)
	GetProvider(ctx context.Context, provider string) (model.ProviderHealth, bool, error)
	SaveProvider(ctx context.Context, provider model.ProviderHealth) error

	ListFailedJobs(ctx context.Context) ([]model.FailedJob, error)
	SearchLeadByPhone(ctx context.Context, phone string) ([]model.LeadSearchResult, error)

	AppendAudit(ctx context.Context, entry model.AuditEntry) error
	ListAudit(ctx context.Context, filter model.AuditFilter) ([]model.AuditEntry, error)

	ListFeatureFlags(ctx context.Context) ([]model.FeatureFlag, error)
	SaveFeatureFlag(ctx context.Context, flag model.FeatureFlag) error

	SaveKillSwitch(ctx context.Context, killSwitch model.KillSwitch) error
	GetKillSwitch(ctx context.Context, scope, scopeID string) (model.KillSwitch, bool, error)

	ListAIQualityItems(ctx context.Context) ([]model.AIQualityItem, error)
}
