package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/lead-identity/internal/model"
)

// Fake is an in-memory Store for testing.
type Fake struct {
	mu       sync.Mutex
	contacts map[string]*model.Contact // key = tenantID+":"+phoneE164
	byID     map[string]*model.Contact
}

func NewFake() *Fake {
	return &Fake{
		contacts: make(map[string]*model.Contact),
		byID:     make(map[string]*model.Contact),
	}
}

func (f *Fake) GetOrCreateContact(ctx context.Context, tenantID, phoneE164, email, name string) (*model.Contact, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + ":" + phoneE164
	if c, ok := f.contacts[key]; ok {
		return c, false, nil
	}
	c := &model.Contact{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		PhoneE164: phoneE164,
		Email:     email,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.contacts[key] = c
	f.byID[c.ID] = c
	return c, true, nil
}

func (f *Fake) GetContact(ctx context.Context, tenantID, contactID string) (*model.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[contactID]
	if !ok || c.TenantID != tenantID {
		return nil, fmt.Errorf("contact not found")
	}
	return c, nil
}

func (f *Fake) MergeContacts(ctx context.Context, tenantID, primaryID, duplicateID, actorID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dup, ok := f.byID[duplicateID]
	if !ok || dup.TenantID != tenantID {
		return fmt.Errorf("duplicate contact not found")
	}
	dup.MergedInto = &primaryID
	dup.UpdatedAt = time.Now()
	// Remove from phone index so the primary wins future lookups.
	key := tenantID + ":" + dup.PhoneE164
	delete(f.contacts, key)
	return nil
}
