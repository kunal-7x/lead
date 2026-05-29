// Package store defines the persistence interface for the telephony adapter.
package store

import (
	"context"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// Store is the data access interface for the telephony adapter.
type Store interface {
	// Call sessions
	StoreCallSession(ctx context.Context, s *model.CallSession) error
	GetCallSession(ctx context.Context, sessionID string) (*model.CallSession, error)
	// GetCallSessionByProviderCallID looks up the session for a provider's call id
	// (e.g. Vobiz CallUUID). Used to resolve the tenant on inbound webhooks, which
	// only carry the provider call id, not our internal session/tenant id.
	GetCallSessionByProviderCallID(ctx context.Context, providerCallID string) (*model.CallSession, error)

	// Provider events
	StoreProviderEvent(ctx context.Context, e *model.CallProviderEvent) error

	// Recordings
	StoreRecording(ctx context.Context, r *model.CallRecording) error
	GetRecording(ctx context.Context, providerCallID string) (*model.CallRecording, error)

	// Providers
	ListProviders(ctx context.Context) ([]*model.Provider, error)
	GetProviderCredentials(ctx context.Context, providerID string) (*model.ProviderCredential, error)

	// Health
	StoreHealthCheck(ctx context.Context, h *model.ProviderHealthCheck) error
	StoreProviderFailure(ctx context.Context, f *model.ProviderFailure) error
	GetProviderFailureRate(ctx context.Context, providerID string, window time.Duration) (float64, error)

	// Routing rules
	ListRoutingRules(ctx context.Context) ([]*model.ProviderRoutingRule, error)
	UpsertRoutingRule(ctx context.Context, rule *model.ProviderRoutingRule) error

	// Webhook dedup
	StoreWebhookEvent(ctx context.Context, e *model.WebhookEvent) error

	// Idempotency
	CheckIdempotencyKey(ctx context.Context, key string) ([]byte, bool, error) // result, exists, err
	SetIdempotencyKey(ctx context.Context, key string, result []byte) error
}
