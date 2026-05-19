package workflows

import (
	"fmt"
	"time"

	"github.com/lead/services/temporal-workers/internal/model"
)

// EvaluateRetryLadder computes the retry schedule for a lead. Pure function,
// deterministic — used by RetryLadderWorkflow via activity and directly in tests.
func EvaluateRetryLadder(input model.RetryLadderInput) (*model.RetryLadderResult, error) {
	if input.LeadID == "" {
		return nil, fmt.Errorf("lead_id required")
	}
	if input.FirstCallAt.IsZero() {
		return nil, fmt.Errorf("first_call_at required")
	}

	policy := input.Policy
	if len(policy.Offsets) == 0 {
		policy = model.DefaultRetryPolicy
	}
	max := policy.Max
	if max <= 0 || max > len(policy.Offsets) {
		max = len(policy.Offsets)
	}

	schedule := make([]model.RetryAttempt, max)
	for i := 0; i < max; i++ {
		schedule[i] = model.RetryAttempt{
			AttemptNumber: i + 1,
			ScheduledAt:   input.FirstCallAt.Add(policy.Offsets[i]).Truncate(time.Second),
		}
	}

	return &model.RetryLadderResult{
		LeadID:   input.LeadID,
		Schedule: schedule,
	}, nil
}
