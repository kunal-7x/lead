package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/lead-identity/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_lead_identity")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) GetOrCreateContact(ctx context.Context, tenantID, phoneE164, email, name string) (*model.Contact, bool, error) {
	phoneKey := tenantID + ":" + phoneE164
	existing, ok, err := pgkv.Get[model.Contact](ctx, p.kv, "contacts_by_phone", phoneKey)
	if err != nil {
		return nil, false, err
	}
	if ok {
		return &existing, false, nil
	}
	now := time.Now().UTC()
	c := model.Contact{
		ID:        pgkv.NewID(""),
		TenantID:  tenantID,
		PhoneE164: phoneE164,
		Email:     email,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := p.kv.Put(ctx, "contacts", c.ID, c); err != nil {
		return nil, false, err
	}
	if err := p.kv.Put(ctx, "contacts_by_phone", phoneKey, c); err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

func (p *PostgresStore) GetContact(ctx context.Context, tenantID, contactID string) (*model.Contact, error) {
	c, ok, err := pgkv.Get[model.Contact](ctx, p.kv, "contacts", contactID)
	if err != nil {
		return nil, err
	}
	if !ok || c.TenantID != tenantID {
		return nil, fmt.Errorf("contact not found")
	}
	return &c, nil
}

func (p *PostgresStore) MergeContacts(ctx context.Context, tenantID, primaryID, duplicateID, actorID string) error {
	dup, err := p.GetContact(ctx, tenantID, duplicateID)
	if err != nil {
		return fmt.Errorf("duplicate contact not found: %w", err)
	}
	dup.MergedInto = &primaryID
	dup.UpdatedAt = time.Now().UTC()
	if err := p.kv.Put(ctx, "contacts", duplicateID, *dup); err != nil {
		return err
	}
	if err := p.kv.Delete(ctx, "contacts_by_phone", tenantID+":"+dup.PhoneE164); err != nil {
		return err
	}
	audit := model.MergeAudit{
		ID:          pgkv.NewID(""),
		TenantID:    tenantID,
		PrimaryID:   primaryID,
		DuplicateID: duplicateID,
		ActorID:     actorID,
		MergedAt:    time.Now().UTC(),
	}
	return p.kv.Put(ctx, "merge_audit", audit.ID, audit)
}
