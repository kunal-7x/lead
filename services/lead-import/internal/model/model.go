package model

import "time"

type LeadSource struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Config    map[string]any `json:"config,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type Contact struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	PhoneE164 string    `json:"phone_e164"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Lead struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ContactID  string    `json:"contact_id"`
	SourceID   string    `json:"source_id,omitempty"`
	Status     string    `json:"status"`
	Score      int       `json:"score"`
	AssignedTo string    `json:"assigned_to,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type LeadImportJob struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	SourceID     string         `json:"source_id,omitempty"`
	FileURL      string         `json:"file_url,omitempty"`
	Mapping      map[string]string `json:"mapping"`
	Status       string         `json:"status"`
	TotalRows    int            `json:"total_rows"`
	ImportedRows int            `json:"imported_rows"`
	ErrorRows    int            `json:"error_rows"`
	Errors       []RowError     `json:"errors,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type RowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

type LeadActivity struct {
	ID         string         `json:"id"`
	LeadID     string         `json:"lead_id"`
	Type       string         `json:"type"`
	Payload    map[string]any `json:"payload,omitempty"`
	ActorID    string         `json:"actor_id,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
}

type LeadStatusHistory struct {
	ID        string    `json:"id"`
	LeadID    string    `json:"lead_id"`
	OldStatus string    `json:"old_status,omitempty"`
	NewStatus string    `json:"new_status"`
	ActorID   string    `json:"actor_id,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}

// ParsedRow is a single normalized row from a CSV/XLSX file.
type ParsedRow struct {
	Fields map[string]string
}

// ColumnMapping maps file column names → canonical field names.
type ColumnMapping map[string]string
