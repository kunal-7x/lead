//go:build temporal

package campaign_test

import (
	"context"
	"testing"

	"github.com/lead/services/campaign/internal/autopause"
	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/store"
)

func seedActiveCampaign(s *store.Fake, id string) {
	c := &model.Campaign{
		ID:       id,
		Name:     "auto-pause test",
		TenantID: "t1",
		Status:   model.StatusActive,
	}
	_ = s.CreateCampaign(context.Background(), c)
}

func TestAutoPause_CostCapReached(t *testing.T) {
	s := store.NewFake()
	seedActiveCampaign(s, "c1")
	_ = s.SetLimits(context.Background(), &model.CampaignLimits{
		CampaignID: "c1",
		CostCapINR: 500,
	})
	_ = s.RecordHealthSnapshot(context.Background(), &model.CampaignHealthSnapshot{
		CampaignID:  "c1",
		CostBurnINR: 500,
	})

	ev := autopause.New(s)
	reason, err := ev.EvaluateAndPause(context.Background(), "c1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason == "" {
		t.Fatal("expected campaign to be paused due to cost cap")
	}

	c, _ := s.GetCampaign(context.Background(), "c1")
	if c.Status != model.StatusPaused {
		t.Fatalf("expected status paused, got %s", c.Status)
	}
}

func TestAutoPause_SuppressionHitRateHigh(t *testing.T) {
	s := store.NewFake()
	seedActiveCampaign(s, "c2")
	_ = s.RecordHealthSnapshot(context.Background(), &model.CampaignHealthSnapshot{
		CampaignID:         "c2",
		SuppressionHitRate: 0.35,
	})

	ev := autopause.New(s)
	reason, err := ev.EvaluateAndPause(context.Background(), "c2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason == "" {
		t.Fatal("expected campaign to be paused due to suppression hit rate")
	}

	c, _ := s.GetCampaign(context.Background(), "c2")
	if c.Status != model.StatusPaused {
		t.Fatalf("expected status paused, got %s", c.Status)
	}
}

func TestAutoPause_HallucinationThreshold(t *testing.T) {
	snap := &model.CampaignHealthSnapshot{
		CampaignID:         "c3",
		HallucinationCount: 6,
	}
	limits := &model.CampaignLimits{}
	should, reason := autopause.CheckHealthSnapshot(snap, limits)
	if !should {
		t.Fatal("expected pause trigger for hallucination count > threshold")
	}
	if !searchString(reason, "hallucination") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestAutoPause_ProviderFailureRate(t *testing.T) {
	snap := &model.CampaignHealthSnapshot{
		CampaignID:          "c4",
		ProviderFailureRate: 0.31,
	}
	limits := &model.CampaignLimits{}
	should, reason := autopause.CheckHealthSnapshot(snap, limits)
	if !should {
		t.Fatal("expected pause trigger for provider failure rate > threshold")
	}
	if !searchString(reason, "provider failure") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestAutoPause_NoPause_WhenHealthy(t *testing.T) {
	s := store.NewFake()
	seedActiveCampaign(s, "c5")
	_ = s.SetLimits(context.Background(), &model.CampaignLimits{
		CampaignID: "c5",
		CostCapINR: 1000,
	})
	_ = s.RecordHealthSnapshot(context.Background(), &model.CampaignHealthSnapshot{
		CampaignID:          "c5",
		CostBurnINR:         100,
		SuppressionHitRate:  0.1,
		HallucinationCount:  2,
		ProviderFailureRate: 0.05,
	})

	ev := autopause.New(s)
	reason, err := ev.EvaluateAndPause(context.Background(), "c5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected no pause for healthy campaign, got: %s", reason)
	}

	c, _ := s.GetCampaign(context.Background(), "c5")
	if c.Status != model.StatusActive {
		t.Fatalf("expected status active, got %s", c.Status)
	}
}

func TestAutoPause_InactiveCampaign_Skipped(t *testing.T) {
	s := store.NewFake()
	_ = s.CreateCampaign(context.Background(), &model.Campaign{
		ID:     "c6",
		Status: model.StatusPaused,
	})
	_ = s.RecordHealthSnapshot(context.Background(), &model.CampaignHealthSnapshot{
		CampaignID:  "c6",
		CostBurnINR: 99999,
	})

	ev := autopause.New(s)
	reason, err := ev.EvaluateAndPause(context.Background(), "c6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected no evaluation for non-active campaign, got: %s", reason)
	}
}

func TestAutoPause_CostCapBillingMeterSignal(t *testing.T) {
	// Simulates the billing-meter signal driving the auto-pause end-to-end.
	// In production, the Temporal workflow would receive a billing-meter signal
	// and call EvaluateAndPause. Here we verify the full path with mocked state.
	s := store.NewFake()
	seedActiveCampaign(s, "c7")
	_ = s.SetLimits(context.Background(), &model.CampaignLimits{
		CampaignID: "c7",
		CostCapINR: 200,
	})

	// Simulate the billing meter signal arriving via a health snapshot update.
	_ = s.RecordHealthSnapshot(context.Background(), &model.CampaignHealthSnapshot{
		CampaignID:  "c7",
		CostBurnINR: 201,
	})

	ev := autopause.New(s)
	reason, err := ev.EvaluateAndPause(context.Background(), "c7")
	if err != nil {
		t.Fatalf("billing meter signal: unexpected error: %v", err)
	}
	if reason == "" {
		t.Fatal("billing meter signal: expected cost-cap pause")
	}

	c, _ := s.GetCampaign(context.Background(), "c7")
	if c.Status != model.StatusPaused {
		t.Fatalf("billing meter signal: expected paused status, got %s", c.Status)
	}
	if !searchString(c.PauseReason, "cost cap") {
		t.Fatalf("billing meter signal: unexpected pause reason: %s", c.PauseReason)
	}
}
