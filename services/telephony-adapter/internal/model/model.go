package model

import "time"

// ProviderCallID is an opaque identifier returned by a telephony provider.
type ProviderCallID string

// CallRequest describes a call to be placed.
type CallRequest struct {
	SessionID    string
	TenantID     string
	FromNumber   string
	ToNumber     string
	CallbackURL  string
	Region       string
	MaxDuration  int // seconds
}

// RecordingRef points to a stored recording.
type RecordingRef struct {
	RecordingID string
	StorageKey  string
	Encrypted   bool
}

// CallSession tracks an outbound call lifecycle.
type CallSession struct {
	ID             string
	TenantID       string
	ProviderCallID string
	ProviderName   string
	FromNumber     string
	ToNumber       string
	Status         string
	StartedAt      time.Time
	EndedAt        *time.Time
	CreatedAt      time.Time
}

// CallProviderEvent is a raw event received from a provider.
type CallProviderEvent struct {
	ID             string
	SessionID      string
	ProviderCallID string
	ProviderName   string
	EventType      string
	RawPayload     []byte
	ReceivedAt     time.Time
}

// CallRecording links a call session to its stored recording.
type CallRecording struct {
	ID             string
	SessionID      string
	ProviderCallID string
	StorageKey     string
	Encrypted      bool
	SizeBytes      int64
	CreatedAt      time.Time
}

// Provider describes a registered telephony provider.
type Provider struct {
	ID       string
	Name     string // "plivo", "exotel", "twilio", "mock"
	Enabled  bool
	Priority int // lower = preferred
}

// ProviderCredential holds auth info for a provider.
type ProviderCredential struct {
	ProviderID string
	AuthID     string
	AuthToken  string
}

// ProviderHealthCheck records a single ping result.
type ProviderHealthCheck struct {
	ID          string
	ProviderID  string
	Healthy     bool
	CheckedAt   time.Time
	LatencyMs   int
}

// ProviderFailure records a provider call failure.
type ProviderFailure struct {
	ID         string
	ProviderID string
	Reason     string
	OccurredAt time.Time
}

// ProviderRoutingRule controls which provider handles a call.
type ProviderRoutingRule struct {
	ID         string
	ProviderID string
	Region     string // empty = all regions
	Priority   int    // lower = preferred
	MaxFailRate float64 // 0-1; skip provider if rolling failure rate exceeds this
}

// WebhookEvent is a deduplicated inbound webhook.
type WebhookEvent struct {
	ID          string
	ProviderName string
	EventType   string
	RawPayload  []byte
	ReceivedAt  time.Time
	Published   bool
}

// IdempotencyKey prevents duplicate processing.
type IdempotencyKey struct {
	Key       string
	Result    []byte
	CreatedAt time.Time
}
