package workflows

import (
	"fmt"
	"time"

	"github.com/lead/services/temporal-workers/internal/model"
)

// EvaluateCallingWindow determines whether a call can proceed now, or when to
// resume. Pure and deterministic — used by CallingWindowGateWorkflow and in tests.
func EvaluateCallingWindow(input model.CallingWindowInput) (*model.CallingWindowResult, error) {
	if input.Timezone == "" {
		input.Timezone = "Asia/Kolkata"
	}

	loc, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", input.Timezone, err)
	}

	local := input.RequestedAt.In(loc)

	windowStart, err := parseHHMM(input.WindowStart, local, loc)
	if err != nil {
		return nil, fmt.Errorf("parse window_start: %w", err)
	}
	windowEnd, err := parseHHMM(input.WindowEnd, local, loc)
	if err != nil {
		return nil, fmt.Errorf("parse window_end: %w", err)
	}

	if !local.Before(windowEnd) && !local.Equal(windowEnd) {
		// After today's window — open next day at window_start.
		next := windowStart.Add(24 * time.Hour)
		return &model.CallingWindowResult{
			CanProceed: false,
			WaitUntil:  next,
			ProceedAt:  next,
		}, nil
	}

	if local.Before(windowStart) {
		return &model.CallingWindowResult{
			CanProceed: false,
			WaitUntil:  windowStart,
			ProceedAt:  windowStart,
		}, nil
	}

	// Within window.
	return &model.CallingWindowResult{
		CanProceed: true,
		ProceedAt:  input.RequestedAt,
	}, nil
}

func parseHHMM(hhmm string, ref time.Time, loc *time.Location) (time.Time, error) {
	var h, m int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil {
		return time.Time{}, fmt.Errorf("expected HH:MM, got %q", hhmm)
	}
	return time.Date(ref.Year(), ref.Month(), ref.Day(), h, m, 0, 0, loc), nil
}
