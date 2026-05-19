package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/lead/services/temporal-workers/internal/activities"
	"github.com/lead/services/temporal-workers/internal/model"
)

// TaskQueue is the Temporal task queue all workers and starters must use.
const TaskQueue = "lead-capsy-task-queue"

// defaultActivityOpts are applied to all activity calls unless overridden.
var defaultActivityOpts = workflow.ActivityOptions{
	StartToCloseTimeout: 15 * time.Second,
	RetryPolicy: &temporal.RetryPolicy{
		MaximumAttempts:        5,
		InitialInterval:        time.Second,
		MaximumInterval:        30 * time.Second,
		BackoffCoefficient:     2,
		NonRetryableErrorTypes: []string{"InvalidInput"},
	},
}

// RetryLadderWorkflow durably schedules call retries for a lead. It computes
// the retry ladder via activity, then sleeps between attempts — surviving
// worker restarts at each timer boundary.
func RetryLadderWorkflow(ctx workflow.Context, input model.RetryLadderInput) (*model.RetryLadderResult, error) {
	if input.LeadID == "" {
		return nil, temporal.NewApplicationError("lead_id required", "InvalidInput")
	}
	if input.FirstCallAt.IsZero() {
		return nil, temporal.NewApplicationError("first_call_at required", "InvalidInput")
	}

	actCtx := workflow.WithActivityOptions(ctx, defaultActivityOpts)

	var result *model.RetryLadderResult
	if err := workflow.ExecuteActivity(actCtx, activities.PickNextCallActivity, input).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("compute retry schedule: %w", err)
	}

	for _, attempt := range result.Schedule {
		if attempt.AttemptNumber <= 1 {
			continue
		}
		waitDur := time.Until(attempt.ScheduledAt)
		if waitDur > 0 {
			if err := workflow.Sleep(ctx, waitDur); err != nil {
				return result, nil // context cancelled/workflow terminated
			}
		}
		if err := workflow.ExecuteActivity(actCtx, activities.SignalSchedulerActivity,
			input.LeadID, input.CampaignID, attempt.AttemptNumber).Get(ctx, nil); err != nil {
			workflow.GetLogger(ctx).Error("signal scheduler failed", "attempt", attempt.AttemptNumber, "error", err)
		}
	}

	return result, nil
}

// CallingWindowGateWorkflow gates a single call attempt on the TRAI calling
// window. It sleeps until the window opens and then signals the scheduler.
func CallingWindowGateWorkflow(ctx workflow.Context, input model.CallingWindowInput) (*model.CallingWindowResult, error) {
	actCtx := workflow.WithActivityOptions(ctx, defaultActivityOpts)

	var result *model.CallingWindowResult
	if err := workflow.ExecuteActivity(actCtx, activities.CheckCallingWindowActivity, input).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("check calling window: %w", err)
	}

	if !result.CanProceed {
		waitDur := time.Until(result.WaitUntil)
		if waitDur > 0 {
			if err := workflow.Sleep(ctx, waitDur); err != nil {
				return result, nil
			}
		}
		// Re-check after sleeping — window may have shifted.
		input.RequestedAt = result.WaitUntil
		if err := workflow.ExecuteActivity(actCtx, activities.CheckCallingWindowActivity, input).Get(ctx, &result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// CampaignHealthWorkflow polls a campaign's health metrics every intervalSeconds
// and auto-pauses the campaign when any threshold is breached.
func CampaignHealthWorkflow(ctx workflow.Context, input model.CampaignHealthInput) error {
	if input.CampaignID == "" {
		return temporal.NewApplicationError("campaign_id required", "InvalidInput")
	}
	if input.IntervalSeconds <= 0 {
		input.IntervalSeconds = 60
	}
	interval := time.Duration(input.IntervalSeconds) * time.Second
	actCtx := workflow.WithActivityOptions(ctx, defaultActivityOpts)

	for {
		if err := workflow.Sleep(ctx, interval); err != nil {
			return nil // workflow cancelled
		}

		var snap model.HealthSnapshot
		if err := workflow.ExecuteActivity(actCtx, activities.GetCampaignHealthActivity,
			input.CampaignID).Get(ctx, &snap); err != nil {
			workflow.GetLogger(ctx).Error("get campaign health failed", "error", err)
			continue
		}

		signal, err := EvaluateCampaignHealth(snap)
		if err != nil {
			workflow.GetLogger(ctx).Error("evaluate campaign health", "error", err)
			continue
		}
		if signal != nil {
			if err := workflow.ExecuteActivity(actCtx, activities.PauseCampaignActivity,
				signal.CampaignID, signal.Reason).Get(ctx, nil); err != nil {
				workflow.GetLogger(ctx).Error("pause campaign failed", "error", err)
			}
			return nil
		}
	}
}

// costCapSignal is the payload sent on the CostCapWatchWorkflow signal channel.
type costCapSignal struct {
	model.BillingMeterEvent
}

// CostCapWatchWorkflow receives billing signals and pauses campaigns that hit
// their cost cap. It runs indefinitely until cancelled.
func CostCapWatchWorkflow(ctx workflow.Context, input model.CostCapWatchInput) error {
	if input.TenantID == "" {
		return temporal.NewApplicationError("tenant_id required", "InvalidInput")
	}

	actCtx := workflow.WithActivityOptions(ctx, defaultActivityOpts)
	sigCh := workflow.GetSignalChannel(ctx, "billing.usage")

	var events []model.BillingMeterEvent
	for {
		var sig costCapSignal
		sigCh.Receive(ctx, &sig)
		if ctx.Err() != nil {
			return nil
		}
		events = append(events, sig.BillingMeterEvent)

		signals, err := EvaluateCostCap(input, events)
		if err != nil {
			continue
		}
		for _, s := range signals {
			if err := workflow.ExecuteActivity(actCtx, activities.PauseCampaignActivity,
				s.CampaignID, s.Reason).Get(ctx, nil); err != nil {
				workflow.GetLogger(ctx).Error("pause campaign from cost cap", "error", err)
			}
		}
	}
}
