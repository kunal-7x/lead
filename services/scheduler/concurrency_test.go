package scheduler_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/model"
	"github.com/lead/services/scheduler/internal/picker"
	"github.com/lead/services/scheduler/internal/store"
)

// TestPickNext_ConcurrentWorkers_NoDoubleClaim verifies that 50 concurrent workers
// picking from the same campaign never claim the same lead twice.
// This tests the atomic lease semantics under contention.
func TestPickNext_ConcurrentWorkers_NoDoubleClaim(t *testing.T) {
	const (
		numLeads   = 5000
		numWorkers = 50
		batchSize  = 20
	)

	s := store.NewFake()
	ctx := context.Background()

	// Seed 5000 leads.
	leads := make([]*model.Lead, numLeads)
	for i := 0; i < numLeads; i++ {
		leads[i] = &model.Lead{
			ID:            fmt.Sprintf("concurrent-lead-%05d", i),
			CampaignID:    "campaign-concurrent",
			TenantID:      "tenant-concurrent",
			PriorityScore: i % 10,
			Status:        model.LeadStatusPending,
		}
	}
	if err := s.AddLeads(ctx, leads); err != nil {
		t.Fatalf("seed leads: %v", err)
	}

	p := picker.NewWithLeaseTTL(s, 10*time.Minute)

	var (
		mu      sync.Mutex
		claimed = make(map[string]string) // leadID -> workerID
		wg      sync.WaitGroup
		errors  []string
	)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-%02d", w)
		go func(wid string) {
			defer wg.Done()
			resp, err := p.PickNext(ctx, model.PickNextRequest{
				CampaignID: "campaign-concurrent",
				WorkerID:   wid,
				BatchSize:  batchSize,
			})
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("worker %s: %v", wid, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, l := range resp.Leads {
				if existing, dup := claimed[l.ID]; dup {
					errors = append(errors, fmt.Sprintf(
						"DOUBLE CLAIM: lead %s claimed by both %s and %s",
						l.ID, existing, wid,
					))
				} else {
					claimed[l.ID] = wid
				}
			}
		}(workerID)
	}

	wg.Wait()

	if len(errors) > 0 {
		for _, e := range errors {
			t.Error(e)
		}
		t.Fatalf("concurrency test failed with %d errors", len(errors))
	}

	t.Logf("concurrent pick: %d unique leads claimed by %d workers (no duplicates)", len(claimed), numWorkers)
}
