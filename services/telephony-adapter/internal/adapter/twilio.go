package adapter

import (
	"context"
	"fmt"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// Twilio is a stub telephony adapter (not yet integrated).
type Twilio struct{}

func NewTwilio() *Twilio { return &Twilio{} }

func (t *Twilio) Name() string { return "twilio" }

func (t *Twilio) Healthy(_ context.Context) bool { return false } // stub: always reports unhealthy until integrated

func (t *Twilio) PlaceCall(_ context.Context, _ model.CallRequest) (model.ProviderCallID, error) {
	return "", fmt.Errorf("twilio: not yet integrated")
}

func (t *Twilio) Hangup(_ context.Context, id model.ProviderCallID) error {
	return fmt.Errorf("twilio: not yet integrated, call %s", id)
}

func (t *Twilio) TransferToHuman(_ context.Context, id model.ProviderCallID, _ string) error {
	return fmt.Errorf("twilio: not yet integrated, call %s", id)
}

func (t *Twilio) GetRecording(_ context.Context, id model.ProviderCallID) (model.RecordingRef, error) {
	return model.RecordingRef{}, fmt.Errorf("twilio: not yet integrated, call %s", id)
}
