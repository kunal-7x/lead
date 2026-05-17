package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/lead/services/scheduler/internal/model"
)

// Fake is an in-memory Store implementation for tests.
// All mutations are protected by a single mutex, which provides the atomicity
// required for the PickAndClaim operation to prevent double-claiming.
type Fake struct {
	mu           sync.Mutex
	leads        map[string]*model.Lead   // keyed by lead ID
	sessions     map[string]string        // callSessionID -> leadID
}

func NewFake() *Fake {
	return &Fake{
		leads:    make(map[string]*model.Lead),
		sessions: make(map[string]string),
	}
}

func (f *Fake) AddLeads(ctx context.Context, leads []*model.Lead) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range leads {
		cp := *l
		if cp.Status == "" {
			cp.Status = model.LeadStatusPending
		}
		f.leads[l.ID] = &cp
	}
	return nil
}

// PickAndClaim is fully atomic under the mutex — no goroutine can claim the
// same lead twice.
func (f *Fake) PickAndClaim(ctx context.Context, campaignID, workerID string, batchSize int, now time.Time, leaseTTL time.Duration) ([]*model.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Collect eligible leads.
	var eligible []*model.Lead
	for _, l := range f.leads {
		if l.CampaignID != campaignID {
			continue
		}
		if l.Status != model.LeadStatusPending {
			continue
		}
		if !l.ProcessingUntil.IsZero() && l.ProcessingUntil.After(now) {
			// Still leased to another worker.
			continue
		}
		eligible = append(eligible, l)
	}

	// Sort: priority DESC, then last_attempt_at ASC (oldest first).
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].PriorityScore != eligible[j].PriorityScore {
			return eligible[i].PriorityScore > eligible[j].PriorityScore
		}
		if eligible[i].LastAttemptAt.Equal(eligible[j].LastAttemptAt) {
			return eligible[i].ID < eligible[j].ID
		}
		return eligible[i].LastAttemptAt.Before(eligible[j].LastAttemptAt)
	})

	if batchSize > len(eligible) {
		batchSize = len(eligible)
	}
	batch := eligible[:batchSize]

	// Atomically stamp lease.
	until := now.Add(leaseTTL)
	result := make([]*model.Lead, len(batch))
	for i, l := range batch {
		l.ProcessingUntil = until
		l.WorkerID = workerID
		l.Status = model.LeadStatusProcessing
		cp := *l
		result[i] = &cp
	}
	return result, nil
}

func (f *Fake) MarkAttempt(ctx context.Context, callSessionID, outcome string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	leadID, ok := f.sessions[callSessionID]
	if !ok {
		return fmt.Errorf("call session %q not found", callSessionID)
	}
	l, ok := f.leads[leadID]
	if !ok {
		return fmt.Errorf("lead %q not found", leadID)
	}

	l.LastAttemptAt = time.Now()
	l.RetryCount++
	l.ProcessingUntil = time.Time{}
	l.WorkerID = ""

	switch outcome {
	case model.OutcomeConverted, model.OutcomeSuppressed, model.OutcomeMaxRetries:
		l.Status = model.LeadStatusDone
	case model.OutcomeFailed:
		l.Status = model.LeadStatusFailed
	default:
		// no_answer, busy, connected → back to pending for retry
		l.Status = model.LeadStatusPending
	}
	return nil
}

func (f *Fake) GetQueueDepth(ctx context.Context, tenantID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, l := range f.leads {
		if l.TenantID == tenantID && l.Status == model.LeadStatusPending {
			count++
		}
	}
	return count, nil
}

func (f *Fake) SetCallSession(ctx context.Context, callSessionID, leadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[callSessionID] = leadID
	return nil
}
