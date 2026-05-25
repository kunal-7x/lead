package adapter

import (
	"context"
	"fmt"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// vobiz is the telephony adapter for the Vobiz REST API (Plivo-family).
type vobiz struct {
	authID    string
	authToken string
	client    VobizHTTPClient
}

// NewVobiz returns a Telephony implementation backed by the Vobiz REST API.
// authID and authToken map to env VOBIZ_AUTH_ID / VOBIZ_AUTH_TOKEN.
func NewVobiz(authID, authToken string, client VobizHTTPClient) Telephony {
	return &vobiz{authID: authID, authToken: authToken, client: client}
}

func (v *vobiz) Name() string { return "vobiz" }

func (v *vobiz) Healthy(ctx context.Context) bool {
	return v.client.CheckHealth(ctx, v.authID)
}

// PlaceCall initiates an outbound call. The CallbackURL in the request is used
// directly as the answer_url (mirroring how the Plivo adapter works).
func (v *vobiz) PlaceCall(ctx context.Context, req model.CallRequest) (model.ProviderCallID, error) {
	uuid, err := v.client.CreateCall(ctx, v.authID, v.authToken, req.FromNumber, req.ToNumber, req.CallbackURL)
	if err != nil {
		return "", fmt.Errorf("vobiz: place call: %w", err)
	}
	return model.ProviderCallID(uuid), nil
}

// Hangup terminates an active call.
func (v *vobiz) Hangup(ctx context.Context, id model.ProviderCallID) error {
	if err := v.client.HangupCall(ctx, v.authID, v.authToken, string(id)); err != nil {
		return fmt.Errorf("vobiz: hangup: %w", err)
	}
	return nil
}

// TransferToHuman redirects the active call's A-leg to targetNumber via the
// Plivo-family transfer endpoint (POST /Call/{uuid}/ with legs+aleg_url).
// NOTE: targetNumber here is treated as a redirect URL (an XML answer URL
// that bridges to the human agent), consistent with how plivo.go's TransferToHuman
// works. If the router needs to construct a full URL from a raw phone number, that
// transformation should happen at the routing layer before calling this method.
func (v *vobiz) TransferToHuman(ctx context.Context, id model.ProviderCallID, targetNumber string) error {
	if err := v.client.TransferCall(ctx, v.authID, v.authToken, string(id), targetNumber); err != nil {
		return fmt.Errorf("vobiz: transfer: %w", err)
	}
	return nil
}

// GetRecording fetches the most recent recording URL for the given call.
func (v *vobiz) GetRecording(ctx context.Context, id model.ProviderCallID) (model.RecordingRef, error) {
	url, err := v.client.GetRecordingURL(ctx, v.authID, v.authToken, string(id))
	if err != nil {
		return model.RecordingRef{}, fmt.Errorf("vobiz: get recording: %w", err)
	}
	return model.RecordingRef{
		RecordingID: fmt.Sprintf("vobiz-%s", id),
		StorageKey:  url,
		Encrypted:   false, // raw provider URL; caller uploads to storage with encryption
	}, nil
}
