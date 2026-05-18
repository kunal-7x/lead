package model

import "time"

type UsageType string

const (
	UsageCallCompleted UsageType = "call.completed"
	UsageWhatsApp      UsageType = "whatsapp.delivered"
	UsageTTS           UsageType = "tts.generated"
	UsageSTT           UsageType = "stt.processed"
	UsageLLM           UsageType = "llm.completion"
)

type UsageEvent struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	CampaignID     string         `json:"campaign_id"`
	Type           UsageType      `json:"type"`
	Quantity       int64          `json:"quantity"`
	Unit           string         `json:"unit"`
	Provider       string         `json:"provider"`
	Category       string         `json:"category"`
	Metadata       map[string]any `json:"metadata"`
	OccurredAt     time.Time      `json:"occurred_at"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type CostEvent struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	CampaignID   string    `json:"campaign_id"`
	UsageEventID string    `json:"usage_event_id"`
	Type         UsageType `json:"type"`
	Quantity     int64     `json:"quantity"`
	Unit         string    `json:"unit"`
	UnitCostINR  float64   `json:"unit_cost_inr"`
	TotalINR     float64   `json:"total_inr"`
	CreatedAt    time.Time `json:"created_at"`
}

type Summary struct {
	ScopeID      string  `json:"scope_id"`
	ScopeType    string  `json:"scope_type"`
	UsageCount   int64   `json:"usage_count"`
	TotalCostINR float64 `json:"total_cost_inr"`
}

type Caps struct {
	TenantID       string  `json:"tenant_id"`
	CampaignID     string  `json:"campaign_id"`
	MonthlyINR     float64 `json:"monthly_inr"`
	MaxCallSeconds int64   `json:"max_call_seconds"`
}

type BillingAccount struct {
	TenantID string `json:"tenant_id"`
	PlanID   string `json:"plan_id"`
}

type Plan struct {
	ID         string  `json:"id"`
	MonthlyINR float64 `json:"monthly_inr"`
}

type CreditLedgerEntry struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	AmountINR float64   `json:"amount_inr"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type Signal struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	CampaignID string         `json:"campaign_id"`
	Type       string         `json:"type"`
	Payload    map[string]any `json:"payload"`
	CreatedAt  time.Time      `json:"created_at"`
}
