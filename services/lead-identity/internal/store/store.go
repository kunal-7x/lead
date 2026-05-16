package store

import (
	"context"

	"github.com/lead/services/lead-identity/internal/model"
)

type Store interface {
	// GetOrCreateContact returns existing contact for (tenant, phone) or creates one.
	GetOrCreateContact(ctx context.Context, tenantID, phoneE164, email, name string) (*model.Contact, bool, error)
	// GetContact fetches a single contact by ID within a tenant.
	GetContact(ctx context.Context, tenantID, contactID string) (*model.Contact, error)
	// MergeContacts makes duplicateID point to primaryID; re-parents all leads.
	MergeContacts(ctx context.Context, tenantID, primaryID, duplicateID, actorID string) error
}
