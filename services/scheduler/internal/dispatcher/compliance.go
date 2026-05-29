package dispatcher

import "time"

// defaultCallWindowStartHour is the earliest hour (inclusive) calls may be placed (TRAI).
const defaultCallWindowStartHour = 10

// defaultCallWindowEndHour is the latest hour (exclusive) calls may be placed (TRAI).
const defaultCallWindowEndHour = 19

// defaultTimezone is the IANA timezone used when none is specified.
const defaultTimezone = "Asia/Kolkata"

// extractWindowParams reads calling-window settings from the row's CampaignCtx.
// Applies defaults when fields are unset or <= 0.
func extractWindowParams(ctx map[string]any) (startH, endH int, loc *time.Location) {
	startH = defaultCallWindowStartHour
	endH = defaultCallWindowEndHour
	tz := defaultTimezone

	if ctx != nil {
		// A window is explicitly configured when end_hour > 0 (end=0 is never a
		// valid window). Then honor start_hour as-is, including 0 (midnight is a
		// valid hour; 0..24 means always open). When end is unset, keep the safe
		// TRAI defaults.
		if ev, ok := ctx["_call_window_end_hour"].(float64); ok && int(ev) > 0 {
			endH = int(ev)
			startH = 0
			if sv, ok := ctx["_call_window_start_hour"].(float64); ok && int(sv) >= 0 {
				startH = int(sv)
			}
		}
		if v, ok := ctx["_timezone"].(string); ok && v != "" {
			tz = v
		}
	}

	var err error
	loc, err = time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return startH, endH, loc
}

// inCallingWindow returns true if now (converted to loc) is in [startH, endH).
func inCallingWindow(now time.Time, startH, endH int, loc *time.Location) bool {
	h := now.In(loc).Hour()
	return h >= startH && h < endH
}

// NextWindowOpen is the exported version of nextWindowOpen for unit tests.
func NextWindowOpen(now time.Time, startH, endH int, loc *time.Location) time.Time {
	return nextWindowOpen(now, startH, endH, loc)
}

// nextWindowOpen returns the next instant at which the calling window opens.
// If now is before today's window-open, returns today at startH.
// If now is inside or after today's window, returns tomorrow at startH.
func nextWindowOpen(now time.Time, startH, endH int, loc *time.Location) time.Time {
	local := now.In(loc)
	h := local.Hour()
	y, mo, d := local.Date()

	if h < startH {
		// Before today's window; open = today at startH:00.
		return time.Date(y, mo, d, startH, 0, 0, 0, loc)
	}
	// Inside window or after — open = tomorrow at startH:00.
	return time.Date(y, mo, d+1, startH, 0, 0, 0, loc)
}

// ----- Retry policy helpers -------------------------------------------------

const (
	defaultRetryMax         = 3
	defaultRetryBusyMin     = 20
	defaultRetryNoAnswerMin = 180
	defaultRetryFailedMin   = 30
)

// retryPolicy holds normalised retry config extracted from CampaignCtx.
type retryPolicy struct {
	RetryMax         int
	RetryBusyMin     int
	RetryNoAnswerMin int
}

// extractRetryPolicy reads retry limits from CampaignCtx and applies defaults.
func extractRetryPolicy(ctx map[string]any) retryPolicy {
	p := retryPolicy{
		RetryMax:         defaultRetryMax,
		RetryBusyMin:     defaultRetryBusyMin,
		RetryNoAnswerMin: defaultRetryNoAnswerMin,
	}
	if ctx == nil {
		return p
	}
	if v, ok := ctx["_retry_max"].(float64); ok && int(v) > 0 {
		p.RetryMax = int(v)
	}
	if v, ok := ctx["_retry_busy_min"].(float64); ok && int(v) > 0 {
		p.RetryBusyMin = int(v)
	}
	if v, ok := ctx["_retry_no_answer_min"].(float64); ok && int(v) > 0 {
		p.RetryNoAnswerMin = int(v)
	}
	return p
}

// retryDelayMinutes returns the retry delay in minutes for a given call status.
// Returns -1 when no retry should occur (status = completed).
func retryDelayMinutes(status string, p retryPolicy) int {
	switch status {
	case "completed":
		return -1 // no retry
	case "busy":
		return p.RetryBusyMin
	case "no-answer":
		return p.RetryNoAnswerMin
	default: // failed, canceled
		return defaultRetryFailedMin
	}
}
