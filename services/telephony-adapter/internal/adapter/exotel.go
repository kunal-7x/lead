package adapter

import (
	"context"
	"fmt"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// Exotel is a stub telephony adapter (not yet integrated).
type Exotel struct{}

func NewExotel() *Exotel { return &Exotel{} }

func (e *Exotel) Name() string { return "exotel" }

func (e *Exotel) Healthy(_ context.Context) bool { return false } // stub: always reports unhealthy until integrated

func (e *Exotel) PlaceCall(_ context.Context, _ model.CallRequest) (model.ProviderCallID, error) {
	return "", fmt.Errorf("exotel: not yet integrated")
}

func (e *Exotel) Hangup(_ context.Context, id model.ProviderCallID) error {
	return fmt.Errorf("exotel: not yet integrated, call %s", id)
}

func (e *Exotel) TransferToHuman(_ context.Context, id model.ProviderCallID, _ string) error {
	return fmt.Errorf("exotel: not yet integrated, call %s", id)
}

func (e *Exotel) GetRecording(_ context.Context, id model.ProviderCallID) (model.RecordingRef, error) {
	return model.RecordingRef{}, fmt.Errorf("exotel: not yet integrated, call %s", id)
}
