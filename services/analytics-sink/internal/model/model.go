package model

import "time"

type CanonicalEvent struct {
	EventID    string         `json:"event_id"`
	TenantID   string         `json:"tenant_id"`
	CampaignID string         `json:"campaign_id"`
	ProjectID  string         `json:"project_id"`
	UserID     string         `json:"user_id"`
	Type       string         `json:"type"`
	Payload    map[string]any `json:"payload"`
	OccurredAt time.Time      `json:"occurred_at"`
}

type FactRow struct {
	EventID    string         `json:"event_id"`
	TenantID   string         `json:"tenant_id"`
	CampaignID string         `json:"campaign_id"`
	ProjectID  string         `json:"project_id"`
	UserID     string         `json:"user_id"`
	Fact       string         `json:"fact"`
	Metric     string         `json:"metric"`
	Value      float64        `json:"value"`
	Payload    map[string]any `json:"payload"`
	OccurredAt time.Time      `json:"occurred_at"`
	InsertedAt time.Time      `json:"inserted_at"`
	Version    int64          `json:"version"`
}

type ReportRow struct {
	Dimension string  `json:"dimension"`
	Count     int64   `json:"count"`
	Value     float64 `json:"value"`
}

type ReportResponse struct {
	TenantID   string      `json:"tenant_id"`
	Report     string      `json:"report"`
	FreshnessS int64       `json:"freshness_s"`
	Rows       []ReportRow `json:"rows"`
}
