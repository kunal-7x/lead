package model

import "time"

type Contact struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	PhoneE164  string     `json:"phone_e164"`
	Email      string     `json:"email,omitempty"`
	Name       string     `json:"name,omitempty"`
	MergedInto *string    `json:"merged_into,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type MergeAudit struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	PrimaryID   string    `json:"primary_id"`
	DuplicateID string    `json:"duplicate_id"`
	ActorID     string    `json:"actor_id,omitempty"`
	MergedAt    time.Time `json:"merged_at"`
}
