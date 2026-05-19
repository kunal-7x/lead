//go:build temporal_replay

package temporalworkers_test

import (
	"testing"
	"time"

	"github.com/lead/services/temporal-workers/internal/model"
	"github.com/lead/services/temporal-workers/internal/workflows"
)

// TestRetryLadder_Determinism verifies that replaying EvaluateRetryLadder
// with identical inputs always produces the same schedule.
func TestRetryLadder_Determinism(t *testing.T) {
	input := model.RetryLadderInput{
		LeadID:      "replay-lead-001",
		CampaignID:  "replay-campaign-001",
		FirstCallAt: time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC),
		Policy:      model.DefaultRetryPolicy,
	}

	first, err := workflows.EvaluateRetryLadder(input)
	if err != nil {
		t.Fatalf("first run error: %v", err)
	}

	for i := 1; i <= 100; i++ {
		result, err := workflows.EvaluateRetryLadder(input)
		if err != nil {
			t.Fatalf("run %d error: %v", i, err)
		}
		if len(result.Schedule) != len(first.Schedule) {
			t.Fatalf("run %d: schedule length mismatch (%d vs %d)", i, len(result.Schedule), len(first.Schedule))
		}
		for j, attempt := range result.Schedule {
			if !attempt.ScheduledAt.Equal(first.Schedule[j].ScheduledAt) {
				t.Fatalf("run %d attempt %d: time mismatch (%v vs %v)",
					i, j+1, attempt.ScheduledAt, first.Schedule[j].ScheduledAt)
			}
			if attempt.AttemptNumber != first.Schedule[j].AttemptNumber {
				t.Fatalf("run %d attempt %d: number mismatch (%d vs %d)",
					i, j+1, attempt.AttemptNumber, first.Schedule[j].AttemptNumber)
			}
		}
	}
	t.Logf("determinism verified: 100 replays of EvaluateRetryLadder produced identical schedules")
}

// TestCallingWindowGate_Determinism verifies EvaluateCallingWindow is deterministic.
func TestCallingWindowGate_Determinism(t *testing.T) {
	input := model.CallingWindowInput{
		CallID:      "replay-call-001",
		RequestedAt: time.Date(2026, 5, 17, 3, 30, 0, 0, time.UTC),
		WindowStart: "09:00",
		WindowEnd:   "21:00",
		Timezone:    "Asia/Kolkata",
	}

	first, err := workflows.EvaluateCallingWindow(input)
	if err != nil {
		t.Fatalf("first run error: %v", err)
	}

	for i := 1; i <= 100; i++ {
		result, err := workflows.EvaluateCallingWindow(input)
		if err != nil {
			t.Fatalf("run %d error: %v", i, err)
		}
		if result.CanProceed != first.CanProceed {
			t.Fatalf("run %d: can_proceed mismatch", i)
		}
		if !result.WaitUntil.Equal(first.WaitUntil) {
			t.Fatalf("run %d: wait_until mismatch", i)
		}
	}
	t.Logf("determinism verified: EvaluateCallingWindow is deterministic")
}
