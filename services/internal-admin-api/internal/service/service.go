package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lead/services/internal-admin-api/internal/model"
	"github.com/lead/services/internal-admin-api/internal/store"
)

var (
	ErrForbidden      = errors.New("forbidden")
	ErrTicketRequired = errors.New("reason and ticket_id are required")
	ErrNotFound       = errors.New("not found")
	ErrBlocked        = errors.New("new provider calls are blocked")
)

type Service struct {
	store store.Store
	now   func() time.Time
	seq   uint64
}

func New(st store.Store) *Service {
	return &Service{
		store: st,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) ListTenants(ctx context.Context, actor model.Actor) ([]model.Tenant, error) {
	if err := requireInternal(actor); err != nil {
		return nil, err
	}
	return s.store.ListTenants(ctx)
}

func (s *Service) SuspendTenant(ctx context.Context, actor model.Actor, tenantID string, req model.ActionRequest) (model.Tenant, error) {
	if err := requireSuperOrOps(actor); err != nil {
		return model.Tenant{}, err
	}
	if err := requireTicket(req); err != nil {
		return model.Tenant{}, err
	}
	tenant, ok, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return model.Tenant{}, err
	}
	if !ok {
		return model.Tenant{}, ErrNotFound
	}
	tenant.Status = model.TenantSuspended
	tenant.UpdatedAt = s.now()
	if err := s.store.SaveTenant(ctx, tenant); err != nil {
		return model.Tenant{}, err
	}
	return tenant, s.audit(ctx, actor, "tenant.suspend", "tenant", tenantID, tenantID, req, nil)
}

func (s *Service) ResumeTenant(ctx context.Context, actor model.Actor, tenantID string, req model.ActionRequest) (model.Tenant, error) {
	if err := requireSuperOrOps(actor); err != nil {
		return model.Tenant{}, err
	}
	tenant, ok, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return model.Tenant{}, err
	}
	if !ok {
		return model.Tenant{}, ErrNotFound
	}
	tenant.Status = model.TenantActive
	tenant.UpdatedAt = s.now()
	if err := s.store.SaveTenant(ctx, tenant); err != nil {
		return model.Tenant{}, err
	}
	return tenant, s.audit(ctx, actor, "tenant.resume", "tenant", tenantID, tenantID, req, nil)
}

func (s *Service) DeleteTenant(ctx context.Context, actor model.Actor, tenantID string, req model.ActionRequest) (model.Tenant, error) {
	if err := requireSuper(actor); err != nil {
		return model.Tenant{}, err
	}
	if err := requireTicket(req); err != nil {
		return model.Tenant{}, err
	}
	tenant, ok, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return model.Tenant{}, err
	}
	if !ok {
		return model.Tenant{}, ErrNotFound
	}
	tenant.Status = model.TenantDeleted
	tenant.UpdatedAt = s.now()
	if err := s.store.SaveTenant(ctx, tenant); err != nil {
		return model.Tenant{}, err
	}
	return tenant, s.audit(ctx, actor, "tenant.soft_delete", "tenant", tenantID, tenantID, req, nil)
}

func (s *Service) ImpersonateTenant(ctx context.Context, actor model.Actor, tenantID string, req model.ActionRequest) (string, error) {
	if err := requireSuper(actor); err != nil {
		return "", err
	}
	if err := requireTicket(req); err != nil {
		return "", err
	}
	if _, ok, err := s.store.GetTenant(ctx, tenantID); err != nil {
		return "", err
	} else if !ok {
		return "", ErrNotFound
	}
	token := fmt.Sprintf("impersonation:%s:%s:%d", actor.ID, tenantID, s.nextID())
	return token, s.audit(ctx, actor, "tenant.impersonate", "tenant", tenantID, tenantID, req, map[string]string{"target_tenant_id": tenantID})
}

func (s *Service) ListProviders(ctx context.Context, actor model.Actor) ([]model.ProviderHealth, error) {
	if err := requireInternal(actor); err != nil {
		return nil, err
	}
	return s.store.ListProviders(ctx)
}

func (s *Service) PauseProvider(ctx context.Context, actor model.Actor, provider string, req model.ActionRequest) (model.KillSwitch, error) {
	if err := requireSuper(actor); err != nil {
		return model.KillSwitch{}, err
	}
	if err := requireTicket(req); err != nil {
		return model.KillSwitch{}, err
	}
	health, ok, err := s.store.GetProvider(ctx, provider)
	if err != nil {
		return model.KillSwitch{}, err
	}
	if !ok {
		return model.KillSwitch{}, ErrNotFound
	}
	drained := health.InFlightCalls
	health.CircuitOpen = true
	health.Online = false
	health.InFlightCalls = 0
	health.LastError = "manual circuit break"
	health.UpdatedAt = s.now()
	if err := s.store.SaveProvider(ctx, health); err != nil {
		return model.KillSwitch{}, err
	}
	killSwitch := model.KillSwitch{
		Scope:        "provider",
		ScopeID:      provider,
		Provider:     provider,
		Active:       true,
		Draining:     drained > 0,
		DrainedCalls: drained,
		Reason:       req.Reason,
		TicketID:     req.TicketID,
		UpdatedAt:    s.now(),
	}
	if err := s.store.SaveKillSwitch(ctx, killSwitch); err != nil {
		return model.KillSwitch{}, err
	}
	return killSwitch, s.audit(ctx, actor, "provider.kill_switch.pause", "provider", provider, "", req, map[string]string{"drained_calls": fmt.Sprint(drained)})
}

func (s *Service) PauseScope(ctx context.Context, actor model.Actor, scope string, scopeID string, req model.ActionRequest) (model.KillSwitch, error) {
	if err := requireSuper(actor); err != nil {
		return model.KillSwitch{}, err
	}
	if err := requireTicket(req); err != nil {
		return model.KillSwitch{}, err
	}
	killSwitch := model.KillSwitch{Scope: scope, ScopeID: scopeID, Active: true, Reason: req.Reason, TicketID: req.TicketID, UpdatedAt: s.now()}
	if err := s.store.SaveKillSwitch(ctx, killSwitch); err != nil {
		return model.KillSwitch{}, err
	}
	return killSwitch, s.audit(ctx, actor, scope+".kill_switch.pause", scope, scopeID, tenantForScope(scope, scopeID), req, nil)
}

func (s *Service) CanStartProviderCall(ctx context.Context, provider string) (bool, error) {
	health, ok, err := s.store.GetProvider(ctx, provider)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrNotFound
	}
	if health.CircuitOpen {
		return false, ErrBlocked
	}
	killSwitch, ok, err := s.store.GetKillSwitch(ctx, "provider", provider)
	if err != nil {
		return false, err
	}
	if ok && killSwitch.Active {
		return false, ErrBlocked
	}
	return true, nil
}

func (s *Service) ListFailedJobs(ctx context.Context, actor model.Actor) ([]model.FailedJob, error) {
	if err := requireInternal(actor); err != nil {
		return nil, err
	}
	return s.store.ListFailedJobs(ctx)
}

func (s *Service) SearchLeadByPhone(ctx context.Context, actor model.Actor, phone string) ([]model.LeadSearchResult, error) {
	if err := requireSuper(actor); err != nil {
		return nil, err
	}
	return s.store.SearchLeadByPhone(ctx, phone)
}

func (s *Service) ListAudit(ctx context.Context, actor model.Actor, filter model.AuditFilter) ([]model.AuditEntry, error) {
	if err := requireInternal(actor); err != nil {
		return nil, err
	}
	return s.store.ListAudit(ctx, filter)
}

func (s *Service) ListFeatureFlags(ctx context.Context, actor model.Actor) ([]model.FeatureFlag, error) {
	if err := requireInternal(actor); err != nil {
		return nil, err
	}
	return s.store.ListFeatureFlags(ctx)
}

func (s *Service) SetFeatureFlag(ctx context.Context, actor model.Actor, flag model.FeatureFlag, req model.ActionRequest) (model.FeatureFlag, error) {
	if err := requireSuperOrOps(actor); err != nil {
		return model.FeatureFlag{}, err
	}
	if flag.Key == "" || flag.TenantID == "" {
		return model.FeatureFlag{}, errors.New("key and tenant_id are required")
	}
	flag.UpdatedAt = s.now()
	if err := s.store.SaveFeatureFlag(ctx, flag); err != nil {
		return model.FeatureFlag{}, err
	}
	return flag, s.audit(ctx, actor, "feature_flag.update", "feature_flag", flag.Key, flag.TenantID, req, map[string]string{"enabled": fmt.Sprint(flag.Enabled)})
}

func (s *Service) ListAIQualityItems(ctx context.Context, actor model.Actor) ([]model.AIQualityItem, error) {
	if err := requireInternal(actor); err != nil {
		return nil, err
	}
	return s.store.ListAIQualityItems(ctx)
}

func (s *Service) audit(ctx context.Context, actor model.Actor, action, targetType, targetID, tenantID string, req model.ActionRequest, metadata map[string]string) error {
	entry := model.AuditEntry{
		ID:         fmt.Sprintf("audit-%d", s.nextID()),
		ActorID:    actor.ID,
		ActorRole:  actor.Role,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		TenantID:   tenantID,
		Reason:     req.Reason,
		TicketID:   req.TicketID,
		Metadata:   metadata,
		CreatedAt:  s.now(),
	}
	return s.store.AppendAudit(ctx, entry)
}

func (s *Service) nextID() uint64 {
	return atomic.AddUint64(&s.seq, 1)
}

func requireInternal(actor model.Actor) error {
	switch actor.Role {
	case model.RoleSuperAdmin, model.RoleOpsAdmin, model.RoleSupportAgent:
		if strings.TrimSpace(actor.ID) == "" {
			return ErrForbidden
		}
		return nil
	default:
		return ErrForbidden
	}
}

func requireSuper(actor model.Actor) error {
	if actor.Role != model.RoleSuperAdmin || strings.TrimSpace(actor.ID) == "" {
		return ErrForbidden
	}
	return nil
}

func requireSuperOrOps(actor model.Actor) error {
	if actor.Role == model.RoleSuperAdmin || actor.Role == model.RoleOpsAdmin {
		if strings.TrimSpace(actor.ID) != "" {
			return nil
		}
	}
	return ErrForbidden
}

func requireTicket(req model.ActionRequest) error {
	if strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.TicketID) == "" {
		return ErrTicketRequired
	}
	return nil
}

func tenantForScope(scope, scopeID string) string {
	if scope == "tenant" {
		return scopeID
	}
	return ""
}
