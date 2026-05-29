package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/lead-import/internal/model"
)

type Fake struct {
	mu         sync.Mutex
	jobs       map[string]*model.LeadImportJob
	contacts   map[string]*model.Contact // key = tenantID+":"+phoneE164
	contactsID map[string]*model.Contact
	leads      map[string]*model.Lead
	idempKeys  map[string]struct{}
}

func NewFake() *Fake {
	return &Fake{
		jobs:       make(map[string]*model.LeadImportJob),
		contacts:   make(map[string]*model.Contact),
		contactsID: make(map[string]*model.Contact),
		leads:      make(map[string]*model.Lead),
		idempKeys:  make(map[string]struct{}),
	}
}

func (f *Fake) CreateImportJob(ctx context.Context, job *model.LeadImportJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	cp := *job
	f.jobs[job.ID] = &cp
	return nil
}

func (f *Fake) GetImportJob(ctx context.Context, tenantID, jobID string) (*model.LeadImportJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[jobID]
	if !ok || j.TenantID != tenantID {
		return nil, fmt.Errorf("job not found")
	}
	cp := *j
	return &cp, nil
}

func (f *Fake) UpdateImportJob(ctx context.Context, job *model.LeadImportJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job.UpdatedAt = time.Now()
	cp := *job
	f.jobs[job.ID] = &cp
	return nil
}

func (f *Fake) UpsertContact(ctx context.Context, c *model.Contact) (*model.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := c.TenantID + ":" + c.PhoneE164
	if existing, ok := f.contacts[key]; ok {
		return existing, nil
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	cp := *c
	f.contacts[key] = &cp
	f.contactsID[c.ID] = &cp
	return &cp, nil
}

func (f *Fake) GetContact(ctx context.Context, tenantID, contactID string) (*model.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.contactsID[contactID]
	if !ok || c.TenantID != tenantID {
		return nil, fmt.Errorf("contact not found")
	}
	cp := *c
	return &cp, nil
}

func (f *Fake) CreateLead(ctx context.Context, lead *model.Lead) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lead.ID == "" {
		lead.ID = uuid.NewString()
	}
	lead.CreatedAt = time.Now()
	lead.UpdatedAt = time.Now()
	cp := *lead
	f.leads[lead.ID] = &cp
	return nil
}

func (f *Fake) GetLead(ctx context.Context, tenantID, leadID string) (*model.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.leads[leadID]
	if !ok || l.TenantID != tenantID {
		return nil, fmt.Errorf("lead not found")
	}
	cp := *l
	return &cp, nil
}

func (f *Fake) ListLeads(ctx context.Context, tenantID string, filters LeadFilters) ([]*model.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.Lead
	for _, l := range f.leads {
		if l.TenantID != tenantID {
			continue
		}
		if filters.Status != "" && l.Status != filters.Status {
			continue
		}
		if filters.AssignedTo != "" && l.AssignedTo != filters.AssignedTo {
			continue
		}
		cp := *l
		out = append(out, &cp)
	}
	return out, nil
}

func (f *Fake) AppendActivity(_ context.Context, _ *model.LeadActivity) error  { return nil }
func (f *Fake) AppendStatusHistory(_ context.Context, _ *model.LeadStatusHistory) error { return nil }

func (f *Fake) CheckAndSetIdempotencyKey(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.idempKeys[key]; ok {
		return true, nil
	}
	f.idempKeys[key] = struct{}{}
	return false, nil
}
