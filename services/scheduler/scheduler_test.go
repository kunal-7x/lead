package scheduler_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/model"
	"github.com/lead/services/scheduler/internal/picker"
	"github.com/lead/services/scheduler/internal/store"
)

func seedLeads(s *store.Fake, campaignID, tenantID string, n int) {
	ctx := context.Background()
	leads := make([]*model.Lead, n)
	for i := 0; i < n; i++ {
		leads[i] = &model.Lead{
			ID:            fmt.Sprintf("lead-%d", i),
			CampaignID:    campaignID,
			TenantID:      tenantID,
			PriorityScore: i % 10,
			Status:        model.LeadStatusPending,
		}
	}
	_ = s.AddLeads(ctx, leads)
}

func TestPickNext_ReturnsLeads(t *testing.T) {
	s := store.NewFake()
	seedLeads(s, "camp-1", "tenant-1", 10)
	p := picker.New(s)

	resp, err := p.PickNext(context.Background(), model.PickNextRequest{
		CampaignID: "camp-1",
		WorkerID:   "w1",
		BatchSize:  5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Leads) != 5 {
		t.Fatalf("expected 5 leads, got %d", len(resp.Leads))
	}
}

func TestPickNext_RespectsBatchSize(t *testing.T) {
	s := store.NewFake()
	seedLeads(s, "camp-2", "tenant-1", 3)
	p := picker.New(s)

	resp, err := p.PickNext(context.Background(), model.PickNextRequest{
		CampaignID: "camp-2",
		WorkerID:   "w1",
		BatchSize:  10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Leads) != 3 {
		t.Fatalf("expected 3 leads (all available), got %d", len(resp.Leads))
	}
}

func TestPickNext_SortsByPriorityDesc(t *testing.T) {
	s := store.NewFake()
	ctx := context.Background()
	_ = s.AddLeads(ctx, []*model.Lead{
		{ID: "a", CampaignID: "camp-3", TenantID: "t1", PriorityScore: 1, Status: model.LeadStatusPending},
		{ID: "b", CampaignID: "camp-3", TenantID: "t1", PriorityScore: 9, Status: model.LeadStatusPending},
		{ID: "c", CampaignID: "camp-3", TenantID: "t1", PriorityScore: 5, Status: model.LeadStatusPending},
	})
	p := picker.New(s)

	resp, err := p.PickNext(ctx, model.PickNextRequest{CampaignID: "camp-3", WorkerID: "w1", BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Leads[0].ID != "b" {
		t.Fatalf("expected highest-priority lead 'b', got %q", resp.Leads[0].ID)
	}
}

func TestPickNext_LeasePreventsDoubleClaimSequential(t *testing.T) {
	s := store.NewFake()
	seedLeads(s, "camp-4", "tenant-1", 2)
	p := picker.NewWithLeaseTTL(s, 30*time.Second)

	r1, err := p.PickNext(context.Background(), model.PickNextRequest{CampaignID: "camp-4", WorkerID: "w1", BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Leads) != 2 {
		t.Fatalf("first pick: expected 2, got %d", len(r1.Leads))
	}

	// Second pick immediately — leads still leased.
	r2, err := p.PickNext(context.Background(), model.PickNextRequest{CampaignID: "camp-4", WorkerID: "w2", BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Leads) != 0 {
		t.Fatalf("second pick during lease: expected 0, got %d", len(r2.Leads))
	}
}

func TestPickNext_EmptyCampaign(t *testing.T) {
	s := store.NewFake()
	p := picker.New(s)

	resp, err := p.PickNext(context.Background(), model.PickNextRequest{
		CampaignID: "camp-empty",
		WorkerID:   "w1",
		BatchSize:  5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Leads) != 0 {
		t.Fatalf("expected 0 leads, got %d", len(resp.Leads))
	}
}

func TestMarkAttempt_OutcomeConnected_ReturnsToPool(t *testing.T) {
	s := store.NewFake()
	ctx := context.Background()
	_ = s.AddLeads(ctx, []*model.Lead{
		{ID: "lead-x", CampaignID: "camp-5", TenantID: "t1", PriorityScore: 5, Status: model.LeadStatusPending},
	})
	_ = s.SetCallSession(ctx, "sess-1", "lead-x")

	p := picker.New(s)
	err := p.MarkAttempt(ctx, model.MarkAttemptRequest{CallSessionID: "sess-1", Outcome: model.OutcomeNoAnswer})
	if err != nil {
		t.Fatal(err)
	}

	// Lead should be back as pending and pickable.
	resp, err := p.PickNext(ctx, model.PickNextRequest{CampaignID: "camp-5", WorkerID: "w2", BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Leads) != 1 {
		t.Fatalf("expected lead back in pool, got %d leads", len(resp.Leads))
	}
}

func TestMarkAttempt_OutcomeConverted_RemovesFromPool(t *testing.T) {
	s := store.NewFake()
	ctx := context.Background()
	_ = s.AddLeads(ctx, []*model.Lead{
		{ID: "lead-y", CampaignID: "camp-6", TenantID: "t1", PriorityScore: 5, Status: model.LeadStatusPending},
	})
	_ = s.SetCallSession(ctx, "sess-2", "lead-y")

	p := picker.New(s)
	_ = p.MarkAttempt(ctx, model.MarkAttemptRequest{CallSessionID: "sess-2", Outcome: model.OutcomeConverted})

	resp, _ := p.PickNext(ctx, model.PickNextRequest{CampaignID: "camp-6", WorkerID: "w3", BatchSize: 1})
	if len(resp.Leads) != 0 {
		t.Fatalf("converted lead should not be re-queued, got %d leads", len(resp.Leads))
	}
}

func TestGetQueueDepth(t *testing.T) {
	s := store.NewFake()
	seedLeads(s, "camp-7", "tenant-depth", 12)
	p := picker.New(s)

	resp, err := p.GetQueueDepth(context.Background(), "tenant-depth")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Depth != 12 {
		t.Fatalf("expected depth 12, got %d", resp.Depth)
	}
}
