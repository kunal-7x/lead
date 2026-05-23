package model

import "time"

type TemplateCategory string

const (
	TemplateCategoryMarketing      TemplateCategory = "marketing"
	TemplateCategoryUtility        TemplateCategory = "utility"
	TemplateCategoryAuthentication TemplateCategory = "authentication"
)

type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

type MessageKind string

const (
	MessageKindText     MessageKind = "text"
	MessageKindTemplate MessageKind = "template"
	MessageKindFlow     MessageKind = "flow"
)

type MessageStatus string

const (
	MessageStatusQueued    MessageStatus = "queued"
	MessageStatusSent      MessageStatus = "sent"
	MessageStatusDelivered MessageStatus = "delivered"
	MessageStatusRead      MessageStatus = "read"
	MessageStatusFailed    MessageStatus = "failed"
	MessageStatusReceived  MessageStatus = "received"
)

type Template struct {
	ID         string           `json:"id"`
	TenantID   string           `json:"tenant_id"`
	Name       string           `json:"name"`
	Language   string           `json:"language"`
	Category   TemplateCategory `json:"category"`
	Body       string           `json:"body"`
	Status     string           `json:"status"`
	MetaName   string           `json:"meta_name"`
	Variables  []string         `json:"variables"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	SyncedAt   *time.Time       `json:"synced_at"`
	RemoteID   string           `json:"remote_id"`
	RemoteNote string           `json:"remote_note"`
}

type Thread struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	LeadID             string    `json:"lead_id"`
	Phone              string    `json:"phone"`
	LastInboundAt      time.Time `json:"last_inbound_at"`
	ServiceWindowUntil time.Time `json:"service_window_until"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Attachment struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

type Message struct {
	ID            string        `json:"id"`
	TenantID      string        `json:"tenant_id"`
	ThreadID      string        `json:"thread_id"`
	LeadID        string        `json:"lead_id"`
	Phone         string        `json:"phone"`
	Direction     Direction     `json:"direction"`
	Kind          MessageKind   `json:"kind"`
	Body          string        `json:"body"`
	TemplateID    string        `json:"template_id"`
	FlowID        string        `json:"flow_id"`
	Attachments   []Attachment  `json:"attachments"`
	Status        MessageStatus `json:"status"`
	MetaMessageID string        `json:"meta_message_id"`
	CreatedAt     time.Time     `json:"created_at"`
}

type WebhookEvent struct {
	ID             string
	TenantID       string
	IdempotencyKey string
	EventType      string
	Payload        []byte
	ReceivedAt     time.Time
}

type WhatsAppOptOut struct {
	ID        string
	TenantID  string
	LeadID    string
	Phone     string
	Channel   string
	Reason    string
	CreatedAt time.Time
}

type OutboxEvent struct {
	ID        string
	TenantID  string
	Subject   string
	Type      string
	Payload   map[string]any
	CreatedAt time.Time
}

type UsageEvent struct {
	ID           string
	TenantID     string
	LeadID       string
	MessageID    string
	EventType    string
	Quantity     int
	UnitCostINR  float64
	TotalCostINR float64
	CreatedAt    time.Time
}

type ConsentLedgerEntry struct {
	ID        string
	TenantID  string
	LeadID    string
	Channel   string
	Basis     string
	Source    string
	Reason    string
	CreatedAt time.Time
}

type VaultCredential struct {
	TenantID          string    `json:"tenant_id"`
	PhoneNumberID     string    `json:"phone_number_id"`
	BusinessAccountID string    `json:"business_account_id"`
	AccessToken       string    `json:"access_token"`
	VerifyToken       string    `json:"verify_token"`
	AppSecret         string    `json:"app_secret"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type RetryTask struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Operation string         `json:"operation"`
	Payload   map[string]any `json:"payload"`
	RunAfter  time.Time      `json:"run_after"`
	Attempts  int            `json:"attempts"`
	LastError string         `json:"last_error"`
	CreatedAt time.Time      `json:"created_at"`
}

type SendTemplateRequest struct {
	TenantID   string            `json:"tenant_id"`
	ProjectID  string            `json:"project_id,omitempty"`
	LeadID     string            `json:"lead_id"`
	Phone      string            `json:"phone"`
	TemplateID string            `json:"template_id"`
	Language   string            `json:"language"`
	Variables  map[string]string `json:"variables"`
}

type SendMessageRequest struct {
	TenantID    string       `json:"tenant_id"`
	ProjectID   string       `json:"project_id,omitempty"`
	ThreadID    string       `json:"thread_id"`
	Body        string       `json:"body"`
	Attachments []Attachment `json:"attachments"`
}

type SendFlowRequest struct {
	TenantID string         `json:"tenant_id"`
	LeadID   string         `json:"lead_id"`
	Phone    string         `json:"phone"`
	FlowID   string         `json:"flow_id"`
	Payload  map[string]any `json:"payload"`
}
