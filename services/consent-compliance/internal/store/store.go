package store

import (
	"context"

	"github.com/lead/services/consent-compliance/internal/model"
)

type Store interface {
	// Consent ledger — append-only.
	RecordConsent(ctx context.Context, rec model.ConsentRecord) error
	// UpdateConsent must always return an error (append-only enforcement).
	UpdateConsent(ctx context.Context, id string) error
	GetConsentTrail(ctx context.Context, leadID string) ([]model.ConsentRecord, error)
	RedactConsentPII(ctx context.Context, ledgerID string) error

	// Withdrawal appends a ledger entry and adds the phone to suppression.
	WithdrawConsent(ctx context.Context, leadID string, channel model.Channel, reason string) error

	// Suppression list.
	IsSuppressed(ctx context.Context, phone string) (bool, error)
	AddSuppression(ctx context.Context, phone, reason string) error
	RemoveSuppression(ctx context.Context, phone, ticketID string) error

	// Opt-outs (per-lead, per-channel).
	IsOptedOut(ctx context.Context, leadID string, channel model.Channel) (bool, error)

	// RERA: get approved knowledge base for a campaign.
	GetApprovedKB(ctx context.Context, campaignID string) (*model.KnowledgeBase, error)

	// Erasure workflow.
	RequestErasure(ctx context.Context, leadID string) (string, error)

	// Lead phone lookup used when withdrawing consent.
	GetLeadPhone(ctx context.Context, leadID string) (string, error)
}
