package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/lead-import/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_lead_import")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) CreateImportJob(ctx context.Context, job *model.LeadImportJob) error {
	now := time.Now().UTC()
	if job.ID == "" {
		job.ID = pgkv.NewID("")
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	return p.kv.Put(ctx, "import_jobs", job.ID, *job)
}

func (p *PostgresStore) GetImportJob(ctx context.Context, tenantID, jobID string) (*model.LeadImportJob, error) {
	job, ok, err := pgkv.Get[model.LeadImportJob](ctx, p.kv, "import_jobs", jobID)
	if err != nil {
		return nil, err
	}
	if !ok || job.TenantID != tenantID {
		return nil, fmt.Errorf("job not found")
	}
	return &job, nil
}

func (p *PostgresStore) UpdateImportJob(ctx context.Context, job *model.LeadImportJob) error {
	job.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "import_jobs", job.ID, *job)
}

func (p *PostgresStore) UpsertContact(ctx context.Context, c *model.Contact) (*model.Contact, error) {
	phoneKey := c.TenantID + ":" + c.PhoneE164
	existing, ok, err := pgkv.Get[model.Contact](ctx, p.kv, "contacts_by_phone", phoneKey)
	if err != nil {
		return nil, err
	}
	if ok {
		return &existing, nil
	}
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = pgkv.NewID("")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if err := p.kv.Put(ctx, "contacts", c.ID, *c); err != nil {
		return nil, err
	}
	if err := p.kv.Put(ctx, "contacts_by_phone", phoneKey, *c); err != nil {
		return nil, err
	}
	return c, nil
}

func (p *PostgresStore) CreateLead(ctx context.Context, lead *model.Lead) error {
	now := time.Now().UTC()
	if lead.ID == "" {
		lead.ID = pgkv.NewID("")
	}
	if lead.CreatedAt.IsZero() {
		lead.CreatedAt = now
	}
	lead.UpdatedAt = now
	return p.kv.Put(ctx, "leads", lead.ID, *lead)
}

func (p *PostgresStore) GetLead(ctx context.Context, tenantID, leadID string) (*model.Lead, error) {
	lead, ok, err := pgkv.Get[model.Lead](ctx, p.kv, "leads", leadID)
	if err != nil {
		return nil, err
	}
	if !ok || lead.TenantID != tenantID {
		return nil, fmt.Errorf("lead not found")
	}
	return &lead, nil
}

func (p *PostgresStore) ListLeads(ctx context.Context, tenantID string, filters LeadFilters) ([]*model.Lead, error) {
	leads, err := pgkv.List[model.Lead](ctx, p.kv, "leads")
	if err != nil {
		return nil, err
	}
	start := filters.Offset
	if start < 0 {
		start = 0
	}
	limit := filters.Limit
	out := make([]*model.Lead, 0, len(leads))
	for _, lead := range leads {
		if lead.TenantID != tenantID {
			continue
		}
		if filters.Status != "" && lead.Status != filters.Status {
			continue
		}
		if filters.SourceID != "" && lead.SourceID != filters.SourceID {
			continue
		}
		if filters.AssignedTo != "" && lead.AssignedTo != filters.AssignedTo {
			continue
		}
		if start > 0 {
			start--
			continue
		}
		cp := lead
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (p *PostgresStore) AppendActivity(ctx context.Context, act *model.LeadActivity) error {
	if act.ID == "" {
		act.ID = pgkv.NewID("")
	}
	if act.OccurredAt.IsZero() {
		act.OccurredAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "lead_activities", act.ID, *act)
}

func (p *PostgresStore) AppendStatusHistory(ctx context.Context, h *model.LeadStatusHistory) error {
	if h.ID == "" {
		h.ID = pgkv.NewID("")
	}
	if h.ChangedAt.IsZero() {
		h.ChangedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "lead_status_history", h.ID, *h)
}

func (p *PostgresStore) CheckAndSetIdempotencyKey(ctx context.Context, key string) (bool, error) {
	inserted, err := p.kv.Insert(ctx, "idempotency_keys", key, map[string]string{"key": key})
	if err != nil {
		return false, err
	}
	return !inserted, nil
}
