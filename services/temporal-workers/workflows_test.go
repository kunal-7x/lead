package temporalworkers_test

import (
	"testing"
	"time"

	"github.com/lead/services/temporal-workers/internal/model"
	"github.com/lead/services/temporal-workers/internal/workflows"
)

// ---- RetryLadderWorkflow tests ----

func TestRetryLadder_DefaultPolicy(t *testing.T) {
	firstCall := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	result, err := workflows.RetryLadderWorkflow(model.RetryLadderInput{
		LeadID:      "lead-1",
		CampaignID:  "camp-1",
		FirstCallAt: firstCall,
		Policy:      model.DefaultRetryPolicy,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LeadID != "lead-1" {
		t.Fatalf("expected lead-1, got %s", result.LeadID)
	}
	if len(result.Schedule) != 5 {
		t.Fatalf("expected 5 attempts, got %d", len(result.Schedule))
	}

	// Verify schedule: immediate, +1h, +24h, +72h, +168h
	expected := []time.Duration{0, time.Hour, 24 * time.Hour, 72 * time.Hour, 168 * time.Hour}
	for i, d := range expected {
		want := firstCall.Add(d).Truncate(time.Second)
		got := result.Schedule[i].ScheduledAt
		if !got.Equal(want) {
			t.Errorf("attempt %d: expected %v, got %v", i+1, want, got)
		}
		if result.Schedule[i].AttemptNumber != i+1 {
			t.Errorf("attempt %d: wrong attempt_number %d", i+1, result.Schedule[i].AttemptNumber)
		}
	}
}

func TestRetryLadder_MissingLeadID_Error(t *testing.T) {
	_, err := workflows.RetryLadderWorkflow(model.RetryLadderInput{
		FirstCallAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for missing lead_id")
	}
}

func TestRetryLadder_MissingFirstCallAt_Error(t *testing.T) {
	_, err := workflows.RetryLadderWorkflow(model.RetryLadderInput{
		LeadID: "lead-x",
	})
	if err == nil {
		t.Fatal("expected error for missing first_call_at")
	}
}

func TestRetryLadder_CustomPolicy(t *testing.T) {
	policy := model.RetryPolicy{
		Offsets: []time.Duration{0, 30 * time.Minute, 2 * time.Hour},
		Max:     3,
	}
	firstCall := time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC)
	result, err := workflows.RetryLadderWorkflow(model.RetryLadderInput{
		LeadID:      "lead-2",
		CampaignID:  "camp-2",
		FirstCallAt: firstCall,
		Policy:      policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Schedule) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(result.Schedule))
	}
}

// ---- CallingWindowGate tests ----

func TestCallingWindowGate_WithinWindow(t *testing.T) {
	// 14:00 IST is within 09:00-21:00 window.
	ist, _ := time.LoadLocation("Asia/Kolkata")
	reqAt := time.Date(2026, 5, 17, 8, 30, 0, 0, time.UTC) // 14:00 IST

	result, err := workflows.CallingWindowGate(model.CallingWindowInput{
		CallID:      "call-1",
		RequestedAt: reqAt,
		WindowStart: "09:00",
		WindowEnd:   "21:00",
		Timezone:    "Asia/Kolkata",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.CanProceed {
		t.Fatalf("expected can_proceed=true for 14:00 IST, got wait_until=%v", result.WaitUntil)
	}
	_ = ist
}

func TestCallingWindowGate_BeforeWindow_WaitsUntil0900IST(t *testing.T) {
	// 06:00 IST is before the 09:00 window.
	ist, _ := time.LoadLocation("Asia/Kolkata")
	reqAt := time.Date(2026, 5, 17, 0, 30, 0, 0, time.UTC) // 06:00 IST

	result, err := workflows.CallingWindowGate(model.CallingWindowInput{
		CallID:      "call-2",
		RequestedAt: reqAt,
		WindowStart: "09:00",
		WindowEnd:   "21:00",
		Timezone:    "Asia/Kolkata",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CanProceed {
		t.Fatal("expected can_proceed=false before window")
	}
	// WaitUntil should be 09:00 IST on the same day.
	want := time.Date(2026, 5, 17, 9, 0, 0, 0, ist)
	if !result.WaitUntil.Equal(want) {
		t.Fatalf("expected wait_until %v, got %v", want, result.WaitUntil)
	}
}

func TestCallingWindowGate_AfterWindow_WaitsNextDay(t *testing.T) {
	// 22:00 IST is after the 21:00 window — should wait until next day 09:00.
	ist, _ := time.LoadLocation("Asia/Kolkata")
	reqAt := time.Date(2026, 5, 17, 16, 30, 0, 0, time.UTC) // 22:00 IST

	result, err := workflows.CallingWindowGate(model.CallingWindowInput{
		CallID:      "call-3",
		RequestedAt: reqAt,
		WindowStart: "09:00",
		WindowEnd:   "21:00",
		Timezone:    "Asia/Kolkata",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CanProceed {
		t.Fatal("expected can_proceed=false after window")
	}
	want := time.Date(2026, 5, 18, 9, 0, 0, 0, ist)
	if !result.WaitUntil.Equal(want) {
		t.Fatalf("expected wait_until %v (next day 09:00 IST), got %v", want, result.WaitUntil)
	}
}

// ---- CostCapWatchWorkflow tests ----

func TestCostCapWatch_CapReached_EmitsSignal(t *testing.T) {
	signals, err := workflows.CostCapWatchWorkflow(
		model.CostCapWatchInput{TenantID: "t1", CapINR: 1000},
		[]model.BillingMeterEvent{
			{TenantID: "t1", CampaignID: "camp-1", CostBurnINR: 1001, CapINR: 1000},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 pause signal, got %d", len(signals))
	}
	if signals[0].CampaignID != "camp-1" {
		t.Fatalf("expected camp-1, got %s", signals[0].CampaignID)
	}
}

func TestCostCapWatch_BelowCap_NoSignal(t *testing.T) {
	signals, err := workflows.CostCapWatchWorkflow(
		model.CostCapWatchInput{TenantID: "t1", CapINR: 1000},
		[]model.BillingMeterEvent{
			{TenantID: "t1", CampaignID: "camp-2", CostBurnINR: 500, CapINR: 1000},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected no signals below cap, got %d", len(signals))
	}
}

func TestCostCapWatch_DifferentTenant_Ignored(t *testing.T) {
	signals, err := workflows.CostCapWatchWorkflow(
		model.CostCapWatchInput{TenantID: "t1", CapINR: 100},
		[]model.BillingMeterEvent{
			{TenantID: "t2", CampaignID: "camp-3", CostBurnINR: 999, CapINR: 100},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected no signals for different tenant, got %d", len(signals))
	}
}

func TestCostCapWatch_NoDuplicateSignals(t *testing.T) {
	signals, err := workflows.CostCapWatchWorkflow(
		model.CostCapWatchInput{TenantID: "t1", CapINR: 100},
		[]model.BillingMeterEvent{
			{TenantID: "t1", CampaignID: "camp-4", CostBurnINR: 200, CapINR: 100},
			{TenantID: "t1", CampaignID: "camp-4", CostBurnINR: 300, CapINR: 100},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 deduplicated signal, got %d", len(signals))
	}
}

// ---- CampaignHealthWorkflow tests ----

func TestCampaignHealth_HallucinationThreshold(t *testing.T) {
	signal, err := workflows.CampaignHealthWorkflow(model.HealthSnapshot{
		CampaignID:         "camp-h1",
		HallucinationCount: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signal == nil {
		t.Fatal("expected pause signal for hallucination count > 5")
	}
}

func TestCampaignHealth_ProviderFailureRate(t *testing.T) {
	signal, err := workflows.CampaignHealthWorkflow(model.HealthSnapshot{
		CampaignID:          "camp-h2",
		ProviderFailureRate: 0.35,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signal == nil {
		t.Fatal("expected pause signal for provider failure rate > 30%")
	}
}

func TestCampaignHealth_SuppressionHitRate(t *testing.T) {
	signal, err := workflows.CampaignHealthWorkflow(model.HealthSnapshot{
		CampaignID:         "camp-h3",
		SuppressionHitRate: 0.35,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signal == nil {
		t.Fatal("expected pause signal for suppression hit rate > 30%")
	}
}

func TestCampaignHealth_Healthy_NoPause(t *testing.T) {
	signal, err := workflows.CampaignHealthWorkflow(model.HealthSnapshot{
		CampaignID:          "camp-h4",
		HallucinationCount:  3,
		ProviderFailureRate: 0.10,
		SuppressionHitRate:  0.10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signal != nil {
		t.Fatalf("expected no pause signal for healthy campaign, got: %+v", signal)
	}
}

func TestCampaignHealth_MissingCampaignID_Error(t *testing.T) {
	_, err := workflows.CampaignHealthWorkflow(model.HealthSnapshot{})
	if err == nil {
		t.Fatal("expected error for missing campaign_id")
	}
}
