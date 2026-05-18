package model

import "time"

type Role string

const (
	RoleSuperAdmin   Role = "super_admin"
	RoleOpsAdmin     Role = "ops_admin"
	RoleSupportAgent Role = "support_agent"
	RoleClientOwner  Role = "client_owner"
)

type Actor struct {
	ID   string `json:"id"`
	Role Role   `json:"role"`
}

type ActionRequest struct {
	Reason   string `json:"reason"`
	TicketID string `json:"ticket_id"`
}

type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
	TenantDeleted   TenantStatus = "deleted"
)

type Tenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Status    TenantStatus `json:"status"`
	Plan      string       `json:"plan"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type ProviderHealth struct {
	Provider           string    `json:"provider"`
	Online             bool      `json:"online"`
	RollingFailureRate float64   `json:"rolling_failure_rate"`
	CircuitOpen        bool      `json:"circuit_open"`
	InFlightCalls      int       `json:"in_flight_calls"`
	P95LatencyMS       int       `json:"p95_latency_ms"`
	LastError          string    `json:"last_error,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type FailedJob struct {
	ID         string    `json:"id"`
	Queue      string    `json:"queue"`
	Subject    string    `json:"subject"`
	WorkflowID string    `json:"workflow_id"`
	TenantID   string    `json:"tenant_id"`
	Error      string    `json:"error"`
	Attempts   int       `json:"attempts"`
	FailedAt   time.Time `json:"failed_at"`
}

type LeadTimelineEvent struct {
	At     time.Time `json:"at"`
	Type   string    `json:"type"`
	Detail string    `json:"detail"`
}

type LeadSearchResult struct {
	LeadID   string              `json:"lead_id"`
	TenantID string              `json:"tenant_id"`
	Phone    string              `json:"phone"`
	Name     string              `json:"name"`
	Status   string              `json:"status"`
	Timeline []LeadTimelineEvent `json:"timeline"`
}

type AuditEntry struct {
	ID         string            `json:"id"`
	ActorID    string            `json:"actor_id"`
	ActorRole  Role              `json:"actor_role"`
	Action     string            `json:"action"`
	TargetType string            `json:"target_type"`
	TargetID   string            `json:"target_id"`
	TenantID   string            `json:"tenant_id,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	TicketID   string            `json:"ticket_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

type AuditFilter struct {
	TenantID string
	ActorID  string
	Action   string
}

type FeatureFlag struct {
	Key         string    `json:"key"`
	TenantID    string    `json:"tenant_id"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type KillSwitch struct {
	Scope        string    `json:"scope"`
	ScopeID      string    `json:"scope_id"`
	Provider     string    `json:"provider,omitempty"`
	Active       bool      `json:"active"`
	Draining     bool      `json:"draining"`
	DrainedCalls int       `json:"drained_calls"`
	Reason       string    `json:"reason"`
	TicketID     string    `json:"ticket_id"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AIQualityItem struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
