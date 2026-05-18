package model

import "time"

type Temperature string

const (
	TemperatureSuperHot Temperature = "super_hot"
	TemperatureHot      Temperature = "hot"
	TemperatureWarm     Temperature = "warm"
	TemperatureCold     Temperature = "cold"
	TemperatureBad      Temperature = "bad"
)

type BuyerType string

const (
	BuyerTypeSelfUse    BuyerType = "self_use"
	BuyerTypeInvestment BuyerType = "investment"
	BuyerTypeBroker     BuyerType = "broker"
	BuyerTypeFake       BuyerType = "fake"
)

type HandoffStatus string

const (
	HandoffStatusPendingAck           HandoffStatus = "pending_ack"
	HandoffStatusAcknowledged         HandoffStatus = "acknowledged"
	HandoffStatusEscalated            HandoffStatus = "escalated"
	HandoffStatusReassigned           HandoffStatus = "reassigned"
	HandoffStatusSuppressionSuggested HandoffStatus = "suppression_suggested"
)

type TaskStatus string

const (
	TaskStatusOpen   TaskStatus = "open"
	TaskStatusClosed TaskStatus = "closed"
)

type Salesperson struct {
	ID               string
	TenantID         string
	TeamID           string
	ManagerID        string
	PerformanceScore float64
	MaxOpenTasks     int
	Active           bool
}

type ScoringSnapshot struct {
	ID                    string
	TenantID              string
	LeadID                string
	CampaignID            string
	TeamID                string
	Temperature           Temperature
	BuyerType             BuyerType
	ShouldCreateSiteVisit bool
}

type CreateHandoffRequest struct {
	TenantID          string `json:"tenant_id"`
	LeadID            string `json:"lead_id"`
	Reason            string `json:"reason"`
	Summary           string `json:"summary"`
	ScoringSnapshotID string `json:"scoring_snapshot_id"`
}

type Handoff struct {
	ID                string        `json:"id"`
	TenantID          string        `json:"tenant_id"`
	LeadID            string        `json:"lead_id"`
	Reason            string        `json:"reason"`
	Summary           string        `json:"summary"`
	ScoringSnapshotID string        `json:"scoring_snapshot_id"`
	AssignedUserID    string        `json:"assigned_user_id"`
	ManagerUserID     string        `json:"manager_user_id"`
	Status            HandoffStatus `json:"status"`
	SLADeadline       time.Time     `json:"sla_deadline"`
	CreatedAt         time.Time     `json:"created_at"`
	AcknowledgedAt    *time.Time    `json:"acknowledged_at,omitempty"`
	EscalatedAt       *time.Time    `json:"escalated_at,omitempty"`
}

type LeadAssignment struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	LeadID     string    `json:"lead_id"`
	UserID     string    `json:"user_id"`
	HandoffID  string    `json:"handoff_id"`
	AssignedAt time.Time `json:"assigned_at"`
}

type Task struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	HandoffID string     `json:"handoff_id"`
	LeadID    string     `json:"lead_id"`
	UserID    string     `json:"user_id"`
	Type      string     `json:"type"`
	Status    TaskStatus `json:"status"`
	DueAt     time.Time  `json:"due_at"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

type TaskEvent struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	TaskID    string         `json:"task_id"`
	HandoffID string         `json:"handoff_id"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type SLAEvent struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	HandoffID string    `json:"handoff_id"`
	Stage     string    `json:"stage"`
	Type      string    `json:"type"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}
