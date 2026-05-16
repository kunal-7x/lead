package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/consent-compliance/internal/model"
)

type Fake struct {
	mu           sync.Mutex
	ledger       map[string]*model.ConsentRecord
	suppressions map[string]*model.Suppression  // key = phone
	optOuts      map[string]bool                // key = leadID:channel
	kbs          map[string]*model.KnowledgeBase // key = campaignID
	leadPhones   map[string]string              // key = leadID
	erasures     map[string]string              // key = leadID → workflowID
}

func NewFake() *Fake {
	return &Fake{
		ledger:       make(map[string]*model.ConsentRecord),
		suppressions: make(map[string]*model.Suppression),
		optOuts:      make(map[string]bool),
		kbs:          make(map[string]*model.KnowledgeBase),
		leadPhones:   make(map[string]string),
		erasures:     make(map[string]string),
	}
}

func (f *Fake) RecordConsent(ctx context.Context, rec model.ConsentRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec.ID == "" {
		rec.ID = uuid.NewString()
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	f.ledger[rec.ID] = &rec
	return nil
}

// UpdateConsent always errors — the consent ledger is append-only.
func (f *Fake) UpdateConsent(_ context.Context, _ string) error {
	return errors.New("consent_ledger is append-only: UPDATE not permitted")
}

func (f *Fake) GetConsentTrail(ctx context.Context, leadID string) ([]model.ConsentRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var recs []model.ConsentRecord
	for _, r := range f.ledger {
		if r.LeadID == leadID {
			recs = append(recs, *r)
		}
	}
	return recs, nil
}

func (f *Fake) RedactConsentPII(ctx context.Context, ledgerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.ledger[ledgerID]
	if !ok {
		return fmt.Errorf("ledger record %q not found", ledgerID)
	}
	now := time.Now()
	rec.PIIRedacted = true
	rec.RedactedAt = &now
	rec.EvidenceURL = "[REDACTED]"
	return nil
}

func (f *Fake) WithdrawConsent(ctx context.Context, leadID string, channel model.Channel, reason string) error {
	if err := f.RecordConsent(ctx, model.ConsentRecord{
		LeadID: leadID,
		Basis:  model.ConsentBasisWithdrawal,
		Source: string(channel),
		NoticeVersion: "withdrawal:" + reason,
	}); err != nil {
		return err
	}
	phone, err := f.GetLeadPhone(ctx, leadID)
	if err == nil && phone != "" {
		return f.AddSuppression(ctx, phone, "consent_withdrawn:"+reason)
	}
	return nil
}

func (f *Fake) IsSuppressed(ctx context.Context, phone string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.suppressions[phone]
	return ok, nil
}

func (f *Fake) AddSuppression(ctx context.Context, phone, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suppressions[phone] = &model.Suppression{
		ID:        uuid.NewString(),
		Phone:     phone,
		Reason:    reason,
		CreatedAt: time.Now(),
	}
	return nil
}

func (f *Fake) RemoveSuppression(ctx context.Context, phone, ticketID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.suppressions[phone]; !ok {
		return fmt.Errorf("phone %q not in suppression list", phone)
	}
	delete(f.suppressions, phone)
	return nil
}

func (f *Fake) IsOptedOut(ctx context.Context, leadID string, channel model.Channel) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := leadID + ":" + string(channel)
	return f.optOuts[key], nil
}

func (f *Fake) GetApprovedKB(ctx context.Context, campaignID string) (*model.KnowledgeBase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kb, ok := f.kbs[campaignID]
	if !ok {
		return nil, nil
	}
	return kb, nil
}

func (f *Fake) RequestErasure(ctx context.Context, leadID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wfID := "erasure-" + uuid.NewString()
	f.erasures[leadID] = wfID
	return wfID, nil
}

func (f *Fake) GetLeadPhone(ctx context.Context, leadID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	phone, ok := f.leadPhones[leadID]
	if !ok {
		return "", fmt.Errorf("lead %q not found", leadID)
	}
	return phone, nil
}

// SetLeadPhone is a test helper.
func (f *Fake) SetLeadPhone(leadID, phone string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leadPhones[leadID] = phone
}

// SetApprovedKB is a test helper.
func (f *Fake) SetApprovedKB(campaignID string, kb *model.KnowledgeBase) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kbs[campaignID] = kb
}

// AddOptOut is a test helper.
func (f *Fake) AddOptOut(leadID string, channel model.Channel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := leadID + ":" + string(channel)
	f.optOuts[key] = true
}
