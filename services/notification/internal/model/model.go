package model

import "time"

type Channel string

const (
	ChannelDashboard Channel = "dashboard"
	ChannelWhatsApp  Channel = "whatsapp"
	ChannelEmail     Channel = "email"
	ChannelSlack     Channel = "slack"
	ChannelDiscord   Channel = "discord"
	ChannelCRM       Channel = "crm_webhook"
)

type Status string

const (
	StatusSent   Status = "sent"
	StatusFailed Status = "failed"
	StatusQueued Status = "queued"
)

type SendRequest struct {
	TenantID  string         `json:"tenant_id"`
	Channel   Channel        `json:"channel"`
	Recipient string         `json:"recipient"`
	Template  string         `json:"template"`
	Payload   map[string]any `json:"payload"`
}

type Notification struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Channel   Channel        `json:"channel"`
	Recipient string         `json:"recipient"`
	Template  string         `json:"template"`
	Payload   map[string]any `json:"payload"`
	Status    Status         `json:"status"`
	Error     string         `json:"error,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type Subscription struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	EventTypes []string  `json:"event_types"`
	Channels   []Channel `json:"channels"`
	Recipients []string  `json:"recipients"`
	Template   string    `json:"template"`
	CreatedAt  time.Time `json:"created_at"`
}

type Event struct {
	TenantID string         `json:"tenant_id"`
	Type     string         `json:"type"`
	Payload  map[string]any `json:"payload"`
}

type RetryTask struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	NotificationID string    `json:"notification_id"`
	Channel        Channel   `json:"channel"`
	Recipient      string    `json:"recipient"`
	RunAfter       time.Time `json:"run_after"`
	Attempts       int       `json:"attempts"`
	LastError      string    `json:"last_error"`
	CreatedAt      time.Time `json:"created_at"`
}
