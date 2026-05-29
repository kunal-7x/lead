package campaign_test

import (
	"context"
	"sort"
	"testing"

	"github.com/lead/services/campaign/internal/campaign"
	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/store"
)

func TestListLeads_ReturnsAttached(t *testing.T) {
	s := store.NewFake()
	svc := campaign.New(s)
	ctx := context.Background()

	campaignID := "camp-abc"

	// Seed the campaign so it exists.
	_ = s.CreateCampaign(ctx, &model.Campaign{
		ID:          campaignID,
		Name:        "lead list test",
		ProjectID:   "p1",
		TenantID:    "t1",
		KbVersionID: "kb1",
	})

	// Attach three leads.
	wantIDs := []string{"lead-1", "lead-2", "lead-3"}
	if err := svc.AttachLeads(ctx, campaignID, wantIDs); err != nil {
		t.Fatalf("AttachLeads: %v", err)
	}

	// List leads back.
	got, err := svc.ListLeads(ctx, campaignID)
	if err != nil {
		t.Fatalf("ListLeads: %v", err)
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("want %d leads, got %d", len(wantIDs), len(got))
	}

	gotIDs := make([]string, len(got))
	for i, l := range got {
		gotIDs[i] = l.LeadID
	}
	sort.Strings(gotIDs)
	sort.Strings(wantIDs)
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Errorf("lead[%d]: want %q got %q", i, wantIDs[i], gotIDs[i])
		}
	}

	// Verify campaign_id is set on each returned record.
	for _, l := range got {
		if l.CampaignID != campaignID {
			t.Errorf("CampaignID: want %q got %q", campaignID, l.CampaignID)
		}
	}
}

func TestListLeads_EmptyWhenNoneAttached(t *testing.T) {
	s := store.NewFake()
	svc := campaign.New(s)
	ctx := context.Background()

	got, err := svc.ListLeads(ctx, "no-such-campaign")
	if err != nil {
		t.Fatalf("ListLeads: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 leads, got %d", len(got))
	}
}
