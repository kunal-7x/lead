// Package adapter defines the Telephony interface and provider implementations.
package adapter

import (
	"context"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// Telephony is the provider-agnostic calling interface.
type Telephony interface {
	PlaceCall(ctx context.Context, req model.CallRequest) (model.ProviderCallID, error)
	Hangup(ctx context.Context, id model.ProviderCallID) error
	TransferToHuman(ctx context.Context, id model.ProviderCallID, targetNumber string) error
	GetRecording(ctx context.Context, id model.ProviderCallID) (model.RecordingRef, error)
	// Name returns the provider name for routing/logging.
	Name() string
	// Healthy returns true if the provider is currently reachable.
	Healthy(ctx context.Context) bool
}
