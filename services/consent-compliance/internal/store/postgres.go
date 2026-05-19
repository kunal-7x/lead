package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/consent-compliance/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_consent_compliance")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) RecordConsent(ctx context.Context, rec model.ConsentRecord) error {
	if rec.ID == "" {
		rec.ID = pgkv.NewID("")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "consent_ledger", rec.ID, rec)
}

func (p *PostgresStore) UpdateConsent(context.Context, string) error {
	return errors.New("consent_ledger is append-only: UPDATE not permitted")
}

func (p *PostgresStore) GetConsentTrail(ctx context.Context, leadID string) ([]model.ConsentRecord, error) {
	records, err := pgkv.List[model.ConsentRecord](ctx, p.kv, "consent_ledger")
	if err != nil {
		return nil, err
	}
	out := make([]model.ConsentRecord, 0, len(records))
	for _, rec := range records {
		if rec.LeadID == leadID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (p *PostgresStore) RedactConsentPII(ctx context.Context, ledgerID string) error {
	rec, ok, err := pgkv.Get[model.ConsentRecord](ctx, p.kv, "consent_ledger", ledgerID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("ledger record %q not found", ledgerID)
	}
	now := time.Now().UTC()
	rec.PIIRedacted = true
	rec.RedactedAt = &now
	rec.EvidenceURL = "[REDACTED]"
	return p.kv.Put(ctx, "consent_ledger", ledgerID, rec)
}

func (p *PostgresStore) WithdrawConsent(ctx context.Context, leadID string, channel model.Channel, reason string) error {
	if err := p.RecordConsent(ctx, model.ConsentRecord{
		LeadID:        leadID,
		Basis:         model.ConsentBasisWithdrawal,
		Source:        string(channel),
		NoticeVersion: "withdrawal:" + reason,
	}); err != nil {
		return err
	}
	phone, err := p.GetLeadPhone(ctx, leadID)
	if err == nil && phone != "" {
		return p.AddSuppression(ctx, phone, "consent_withdrawn:"+reason)
	}
	return nil
}

func (p *PostgresStore) IsSuppressed(ctx context.Context, phone string) (bool, error) {
	_, ok, err := pgkv.Get[model.Suppression](ctx, p.kv, "suppressions", phone)
	return ok, err
}

func (p *PostgresStore) AddSuppression(ctx context.Context, phone, reason string) error {
	s := model.Suppression{
		ID:        pgkv.NewID(""),
		Phone:     phone,
		Reason:    reason,
		CreatedAt: time.Now().UTC(),
	}
	return p.kv.Put(ctx, "suppressions", phone, s)
}

func (p *PostgresStore) RemoveSuppression(ctx context.Context, phone, ticketID string) error {
	s, ok, err := pgkv.Get[model.Suppression](ctx, p.kv, "suppressions", phone)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("phone %q not in suppression list", phone)
	}
	s.TicketID = ticketID
	if err := p.kv.Put(ctx, "suppression_removals", s.ID, s); err != nil {
		return err
	}
	return p.kv.Delete(ctx, "suppressions", phone)
}

func (p *PostgresStore) IsOptedOut(ctx context.Context, leadID string, channel model.Channel) (bool, error) {
	_, ok, err := pgkv.Get[model.WhatsAppOptOut](ctx, p.kv, "opt_outs", leadID+":"+string(channel))
	return ok, err
}

func (p *PostgresStore) GetApprovedKB(ctx context.Context, campaignID string) (*model.KnowledgeBase, error) {
	kb, ok, err := pgkv.Get[model.KnowledgeBase](ctx, p.kv, "approved_kbs", campaignID)
	if err != nil || !ok {
		return nil, err
	}
	return &kb, nil
}

func (p *PostgresStore) RequestErasure(ctx context.Context, leadID string) (string, error) {
	wfID := "erasure-" + pgkv.NewID("")
	return wfID, p.kv.Put(ctx, "erasures", leadID, map[string]string{"lead_id": leadID, "workflow_id": wfID})
}

func (p *PostgresStore) GetLeadPhone(ctx context.Context, leadID string) (string, error) {
	value, ok, err := pgkv.Get[map[string]string](ctx, p.kv, "lead_phones", leadID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("lead %q not found", leadID)
	}
	return value["phone"], nil
}
