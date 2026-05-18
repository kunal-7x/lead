package model

import "time"

// Lead status constants for schedulable leads.
const (
	LeadStatusPending    = "pending"
	LeadStatusProcessing = "processing"
	LeadStatusDone       = "done"
	LeadStatusFailed     = "failed"
)

// Outcome constants for MarkAttempt.
const (
	OutcomeConnected  = "connected"
	OutcomeNoAnswer   = "no_answer"
	OutcomeBusy       = "busy"
	OutcomeFailed     = "failed"
	OutcomeConverted  = "converted"
	OutcomeSuppressed = "suppressed"
	OutcomeMaxRetries = "max_retries"
)

// Lead is a schedulable outbound call target.
type Lead struct {
	ID              string    `json:"id"`
	CampaignID      string    `json:"campaign_id"`
	TenantID        string    `json:"tenant_id"`
	PriorityScore   int       `json:"priority_score"`
	LastAttemptAt   time.Time `json:"last_attempt_at"`
	RetryCount      int       `json:"retry_count"`
	Status          string    `json:"status"`
	ProcessingUntil time.Time `json:"processing_until"`
	WorkerID        string    `json:"worker_id,omitempty"`
}

// PickNextRequest specifies parameters for atomic lead claiming.
type PickNextRequest struct {
	CampaignID string `json:"campaign_id"`
	WorkerID   string `json:"worker_id"`
	BatchSize  int    `json:"batch_size"`
}

// PickNextResponse holds atomically claimed leads.
type PickNextResponse struct {
	Leads []*Lead `json:"leads"`
}

// MarkAttemptRequest records a call outcome for a lead.
type MarkAttemptRequest struct {
	CallSessionID string `json:"call_session_id"`
	Outcome       string `json:"outcome"`
}

// QueueDepthResponse returns the number of pending leads for a tenant.
type QueueDepthResponse struct {
	TenantID string `json:"tenant_id"`
	Depth    int    `json:"depth"`
}

type DemoDispatchLead struct {
	ID            string `json:"id"`
	ToNumber      string `json:"to_number"`
	PriorityScore int    `json:"priority_score"`
}

type DemoDispatchRequest struct {
	TenantID    string             `json:"tenant_id"`
	CampaignID  string             `json:"campaign_id"`
	WorkerID    string             `json:"worker_id"`
	BatchSize   int                `json:"batch_size"`
	FromNumber  string             `json:"from_number"`
	Region      string             `json:"region"`
	MaxDuration int                `json:"max_duration"`
	Demo        bool               `json:"demo"`
	Leads       []DemoDispatchLead `json:"leads"`
}

type DemoDispatchAttempt struct {
	LeadID         string `json:"lead_id"`
	SessionID      string `json:"session_id"`
	Provider       string `json:"provider"`
	ProviderCallID string `json:"provider_call_id"`
	Status         string `json:"status"`
}

type DemoDispatchResponse struct {
	CampaignID string                `json:"campaign_id"`
	Attempted  int                   `json:"attempted"`
	Calls      []DemoDispatchAttempt `json:"calls"`
}
