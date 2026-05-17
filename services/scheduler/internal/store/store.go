package store

import (
	"context"
	"time"

	"github.com/lead/services/scheduler/internal/model"
)

// Store is the persistence layer for the scheduler.
type Store interface {
	// AddLeads inserts leads into the queue (used for seeding and campaign attachment).
	AddLeads(ctx context.Context, leads []*model.Lead) error

	// PickAndClaim atomically selects up to batchSize eligible leads for campaignID,
	// sets their ProcessingUntil to leaseTTL ahead, and returns them. Eligible means
	// status=pending and ProcessingUntil before now. Ordering: priority DESC, last_attempt_at ASC.
	PickAndClaim(ctx context.Context, campaignID, workerID string, batchSize int, now time.Time, leaseTTL time.Duration) ([]*model.Lead, error)

	// MarkAttempt records an outcome for the lead associated with callSessionID and
	// advances its retry state.
	MarkAttempt(ctx context.Context, callSessionID, outcome string) error

	// GetQueueDepth returns the number of pending leads for a tenant.
	GetQueueDepth(ctx context.Context, tenantID string) (int, error)

	// SetCallSession maps a callSessionID to a leadID (called when a call is created).
	SetCallSession(ctx context.Context, callSessionID, leadID string) error
}
