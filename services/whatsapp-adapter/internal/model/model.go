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
	ID         string
	TenantID   string
	Name       string
	Language   string
	Category   TemplateCategory
	Body       string
	Status     string
	MetaName   string
	Variables  []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	SyncedAt   *time.Time
	RemoteID   string
	RemoteNote string
}

type Thread struct {
	ID                 string
	TenantID           string
	LeadID             string
	Phone              string
	LastInboundAt      time.Time
	ServiceWindowUntil time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Attachment struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

type Message struct {
	ID            string
	TenantID      string
	ThreadID      string
	LeadID        string
	Phone         string
	Direction     Direction
	Kind          MessageKind
	Body          string
	TemplateID    string
	FlowID        string
	Attachments   []Attachment
	Status        MessageStatus
	MetaMessageID string
	CreatedAt     time.Time
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
	TenantID          string
	PhoneNumberID     string
	BusinessAccountID string
	AccessToken       string
	VerifyToken       string
	AppSecret         string
	UpdatedAt         time.Time
}

type RetryTask struct {
	ID        string
	TenantID  string
	Operation string
	Payload   map[string]any
	RunAfter  time.Time
	Attempts  int
	LastError string
	CreatedAt time.Time
}

type SendTemplateRequest struct {
	TenantID   string
	LeadID     string
	Phone      string
	TemplateID string
	Language   string
	Variables  map[string]string
}

type SendMessageRequest struct {
	TenantID    string
	ThreadID    string
	Body        string
	Attachments []Attachment
}

type SendFlowRequest struct {
	TenantID string
	LeadID   string
	Phone    string
	FlowID   string
	Payload  map[string]any
}
