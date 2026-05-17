package model

import "time"

// RetryPolicy defines the retry schedule offsets relative to the first attempt.
type RetryPolicy struct {
	Offsets []time.Duration // e.g. [0, 1h, 24h, 72h, 168h]
	Max     int             // maximum retry count
}

// DefaultRetryPolicy is the standard ladder: immediate, +1h, +1d, +3d, +7d.
var DefaultRetryPolicy = RetryPolicy{
	Offsets: []time.Duration{
		0,
		1 * time.Hour,
		24 * time.Hour,
		72 * time.Hour,
		168 * time.Hour,
	},
	Max: 5,
}

// RetryAttempt is a scheduled retry entry.
type RetryAttempt struct {
	AttemptNumber int       `json:"attempt_number"`
	ScheduledAt   time.Time `json:"scheduled_at"`
}

// RetryLadderInput is the input to RetryLadderWorkflow.
type RetryLadderInput struct {
	LeadID      string      `json:"lead_id"`
	CampaignID  string      `json:"campaign_id"`
	FirstCallAt time.Time   `json:"first_call_at"`
	Policy      RetryPolicy `json:"policy"`
}

// RetryLadderResult contains the computed retry schedule.
type RetryLadderResult struct {
	LeadID   string         `json:"lead_id"`
	Schedule []RetryAttempt `json:"schedule"`
}

// CallingWindowInput is the input to CallingWindowGate.
type CallingWindowInput struct {
	CallID      string    `json:"call_id"`
	RequestedAt time.Time `json:"requested_at"`
	WindowStart string    `json:"window_start"` // e.g. "09:00"
	WindowEnd   string    `json:"window_end"`   // e.g. "21:00"
	Timezone    string    `json:"timezone"`     // e.g. "Asia/Kolkata"
}

// CallingWindowResult indicates whether the call can proceed and when.
type CallingWindowResult struct {
	CanProceed bool      `json:"can_proceed"`
	ProceedAt  time.Time `json:"proceed_at"`
	WaitUntil  time.Time `json:"wait_until,omitempty"`
}

// BillingMeterEvent signals a cost threshold event from the billing plane.
type BillingMeterEvent struct {
	TenantID    string  `json:"tenant_id"`
	CampaignID  string  `json:"campaign_id"`
	CostBurnINR float64 `json:"cost_burn_inr"`
	CapINR      float64 `json:"cap_inr"`
}

// CostCapWatchInput is the input to CostCapWatchWorkflow.
type CostCapWatchInput struct {
	TenantID string  `json:"tenant_id"`
	CapINR   float64 `json:"cap_inr"`
}

// CampaignPauseSignal is emitted when a campaign must be auto-paused.
type CampaignPauseSignal struct {
	CampaignID string `json:"campaign_id"`
	Reason     string `json:"reason"`
}

// HealthSnapshot is a point-in-time health reading for a campaign.
type HealthSnapshot struct {
	CampaignID          string  `json:"campaign_id"`
	ConnectRate         float64 `json:"connect_rate"`
	QualifyRate         float64 `json:"qualify_rate"`
	CostBurnINR         float64 `json:"cost_burn_inr"`
	SuppressionHitRate  float64 `json:"suppression_hit_rate"`
	HallucinationCount  int     `json:"hallucination_count"`
	ProviderFailureRate float64 `json:"provider_failure_rate"`
}

// CampaignHealthInput is the input to CampaignHealthWorkflow.
type CampaignHealthInput struct {
	CampaignID      string `json:"campaign_id"`
	IntervalSeconds int    `json:"interval_seconds"`
}
