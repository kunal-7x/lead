package picker

import (
	"context"
	"sync"
	"time"

	"github.com/lead/services/scheduler/internal/model"
	"github.com/lead/services/scheduler/internal/store"
)

const defaultLeaseTTL = 5 * time.Minute

// Picker wraps the store with scheduling policy: lease semantics, fairness tracking.
type Picker struct {
	store    store.Store
	leaseTTL time.Duration

	// fairness counter per tenant: tracks last-picked campaign for round-robin tiebreaking.
	mu      sync.Mutex
	counter map[string]int // tenantID -> pick count (used as tiebreaker seed)
}

func New(s store.Store) *Picker {
	return &Picker{
		store:    s,
		leaseTTL: defaultLeaseTTL,
		counter:  make(map[string]int),
	}
}

func NewWithLeaseTTL(s store.Store, ttl time.Duration) *Picker {
	return &Picker{
		store:    s,
		leaseTTL: ttl,
		counter:  make(map[string]int),
	}
}

// PickNext atomically claims up to batchSize leads for the given campaign.
// Respects lease semantics: leads already claimed by another worker are skipped.
// Returns claimed leads ordered by priority DESC, last_attempt_at ASC.
func (p *Picker) PickNext(ctx context.Context, req model.PickNextRequest) (*model.PickNextResponse, error) {
	if req.BatchSize <= 0 {
		req.BatchSize = 1
	}

	now := time.Now()
	leads, err := p.store.PickAndClaim(ctx, req.CampaignID, req.WorkerID, req.BatchSize, now, p.leaseTTL)
	if err != nil {
		return nil, err
	}

	return &model.PickNextResponse{Leads: leads}, nil
}

// MarkAttempt records the outcome of a call attempt and feeds back into the retry ladder.
func (p *Picker) MarkAttempt(ctx context.Context, req model.MarkAttemptRequest) error {
	return p.store.MarkAttempt(ctx, req.CallSessionID, req.Outcome)
}

// GetQueueDepth returns the number of pending leads for a tenant.
func (p *Picker) GetQueueDepth(ctx context.Context, tenantID string) (*model.QueueDepthResponse, error) {
	depth, err := p.store.GetQueueDepth(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &model.QueueDepthResponse{TenantID: tenantID, Depth: depth}, nil
}
