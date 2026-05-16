package store

import (
	"context"

	"github.com/lead/services/lead-import/internal/model"
)

type Store interface {
	// Import jobs
	CreateImportJob(ctx context.Context, job *model.LeadImportJob) error
	GetImportJob(ctx context.Context, tenantID, jobID string) (*model.LeadImportJob, error)
	UpdateImportJob(ctx context.Context, job *model.LeadImportJob) error

	// Contacts (with dedup on tenant+phone)
	UpsertContact(ctx context.Context, c *model.Contact) (*model.Contact, error)

	// Leads
	CreateLead(ctx context.Context, lead *model.Lead) error
	GetLead(ctx context.Context, tenantID, leadID string) (*model.Lead, error)
	ListLeads(ctx context.Context, tenantID string, filters LeadFilters) ([]*model.Lead, error)

	// Activities and status history (append-only)
	AppendActivity(ctx context.Context, act *model.LeadActivity) error
	AppendStatusHistory(ctx context.Context, h *model.LeadStatusHistory) error

	// Idempotency for webhooks
	CheckAndSetIdempotencyKey(ctx context.Context, key string) (alreadyExists bool, err error)
}

type LeadFilters struct {
	Status     string
	SourceID   string
	AssignedTo string
	Limit      int
	Offset     int
}
