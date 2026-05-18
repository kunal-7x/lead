package model

import "time"

type State string

const (
	StateTentative    State = "tentative"
	StateConfirmed    State = "confirmed"
	StateReminderSent State = "reminder_sent"
	StateCompleted    State = "completed"
	StateNoShow       State = "no_show"
	StateRescheduled  State = "rescheduled"
	StateLost         State = "lost"
)

type ProposedSlot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Visit struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	LeadID        string         `json:"lead_id"`
	ProjectID     string         `json:"project_id"`
	ProposedSlots []ProposedSlot `json:"proposed_slots"`
	ConfirmedSlot ProposedSlot   `json:"confirmed_slot"`
	SalesRepID    string         `json:"sales_rep_id"`
	State         State          `json:"state"`
	Notes         string         `json:"notes,omitempty"`
	AttendeeCount int            `json:"attendee_count,omitempty"`
	LossReason    string         `json:"loss_reason,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Event struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	VisitID   string         `json:"visit_id"`
	Type      string         `json:"type"`
	FromState State          `json:"from_state"`
	ToState   State          `json:"to_state"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type WorkflowAction struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	VisitID   string    `json:"visit_id"`
	Type      string    `json:"type"`
	DueAt     time.Time `json:"due_at"`
	FiredAt   time.Time `json:"fired_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTentativeRequest struct {
	TenantID      string         `json:"tenant_id"`
	LeadID        string         `json:"lead_id"`
	ProjectID     string         `json:"project_id"`
	ProposedSlots []ProposedSlot `json:"proposed_slots"`
}
