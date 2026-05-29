package model

import "time"

// Campaign statuses.
const (
	StatusDraft           = "draft"
	StatusActive          = "active"
	StatusPaused          = "paused"
	StatusArchived        = "archived"
	StatusPreflightFailed = "preflight_failed"
)

// ProjectTypes.
const (
	ProjectTypeRealEstate = "real-estate"
)

type ObjectionResponse struct {
	Objection string `json:"objection"`
	Response  string `json:"response"`
}

type CampaignContext struct {
	ProductDescription  string              `json:"product_description"`
	Offer               string              `json:"offer"`
	TalkingPoints       []string            `json:"talking_points"`
	ObjectionHandling   []ObjectionResponse `json:"objection_handling"`
	QualifyingQuestions []string            `json:"qualifying_questions"`
	Persona             string              `json:"persona"`
	DoNotSay            []string            `json:"do_not_say"`
	Goal                string              `json:"goal"`
	Language            string              `json:"language"`
	BusinessHours       string              `json:"business_hours"`
}

type Campaign struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	ProjectID       string          `json:"project_id"`
	TenantID        string          `json:"tenant_id"`
	KbVersionID     string          `json:"kb_version_id"`
	ScriptVersionID string          `json:"script_version_id"`
	PromptVersionID string          `json:"prompt_version_id"`
	SourceFilter    string          `json:"source_filter"`
	Schedule        string          `json:"schedule"`
	Status          string          `json:"status"`
	PauseReason     string          `json:"pause_reason,omitempty"`
	Context         CampaignContext `json:"context"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type CampaignSettings struct {
	ID         string    `json:"id"`
	CampaignID string    `json:"campaign_id"`
	Schedule   string    `json:"schedule"`
	CreatedAt  time.Time `json:"created_at"`
}

type CampaignLead struct {
	CampaignID string    `json:"campaign_id"`
	LeadID     string    `json:"lead_id"`
	AttachedAt time.Time `json:"attached_at"`
}

type CampaignLimits struct {
	CampaignID       string    `json:"campaign_id"`
	DailyCallCap     int       `json:"daily_call_cap"`
	ConcurrentCap    int       `json:"concurrent_cap"`
	HourlyCallCap    int       `json:"hourly_call_cap"`
	RetryMax         int       `json:"retry_max"`
	RetryBusyMin     int       `json:"retry_busy_min"`
	RetryNoAnswerMin int       `json:"retry_no_answer_min"`
	CostCapINR       float64   `json:"cost_cap_inr"`
	MaxCallSeconds   int       `json:"max_call_seconds"`
	// Calling-window compliance (TRAI). Defaults: 10–19 IST.
	CallWindowStartHour int    `json:"call_window_start_hour"`
	CallWindowEndHour   int    `json:"call_window_end_hour"`
	Timezone            string `json:"timezone"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CampaignHealthSnapshot struct {
	ID                string    `json:"id"`
	CampaignID        string    `json:"campaign_id"`
	ConnectRate       float64   `json:"connect_rate"`
	QualifyRate       float64   `json:"qualify_rate"`
	CostBurnINR       float64   `json:"cost_burn_inr"`
	SuppressionHitRate float64  `json:"suppression_hit_rate"`
	HallucinationCount int      `json:"hallucination_count"`
	ProviderFailureRate float64 `json:"provider_failure_rate"`
	SnapshotAt        time.Time `json:"snapshot_at"`
}

type CampaignScript struct {
	ID         string    `json:"id"`
	CampaignID string    `json:"campaign_id"`
	Version    string    `json:"version"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type CampaignPromptVersion struct {
	ID         string    `json:"id"`
	CampaignID string    `json:"campaign_id"`
	Version    string    `json:"version"`
	Prompt     string    `json:"prompt"`
	CreatedAt  time.Time `json:"created_at"`
}

type CampaignHealth struct {
	CampaignID         string  `json:"campaign_id"`
	ConnectRate        float64 `json:"connect_rate"`
	QualifyRate        float64 `json:"qualify_rate"`
	CostBurnINR        float64 `json:"cost_burn_inr"`
	SuppressionHitRate float64 `json:"suppression_hit_rate"`
}

type PreflightResult struct {
	Passed bool     `json:"passed"`
	Errors []string `json:"errors,omitempty"`
}

// KbVersion is a minimal view of KB version state needed by campaign preflight.
type KbVersion struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	ProjectType string `json:"project_type"`
	HasRERA     bool   `json:"has_rera"`
	HasBrochure bool   `json:"has_brochure"`
	HasPriceSheet bool `json:"has_price_sheet"`
}

// TenantStatus represents billing/suspension state of a tenant.
type TenantStatus struct {
	Suspended     bool    `json:"suspended"`
	BillingCapINR float64 `json:"billing_cap_inr"`
	UsedINR       float64 `json:"used_inr"`
}
