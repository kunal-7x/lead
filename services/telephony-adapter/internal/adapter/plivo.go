package adapter

import (
	"context"
	"fmt"

	"github.com/lead/services/telephony-adapter/internal/model"
)

// PlivoHTTPClient abstracts the Plivo REST API for testability.
type PlivoHTTPClient interface {
	CreateCall(ctx context.Context, authID, authToken, from, to, answerURL string) (string, error)
	HangupCall(ctx context.Context, authID, authToken, callUUID string) error
	TransferCall(ctx context.Context, authID, authToken, callUUID, target string) error
	GetRecordingURL(ctx context.Context, authID, authToken, callUUID string) (string, error)
	CheckHealth(ctx context.Context) bool
}

// Plivo is the primary telephony adapter using the Plivo REST API.
type Plivo struct {
	authID    string
	authToken string
	client    PlivoHTTPClient
}

func NewPlivo(authID, authToken string, client PlivoHTTPClient) *Plivo {
	return &Plivo{authID: authID, authToken: authToken, client: client}
}

func (p *Plivo) Name() string { return "plivo" }

func (p *Plivo) Healthy(ctx context.Context) bool {
	return p.client.CheckHealth(ctx)
}

func (p *Plivo) PlaceCall(ctx context.Context, req model.CallRequest) (model.ProviderCallID, error) {
	uuid, err := p.client.CreateCall(ctx, p.authID, p.authToken, req.FromNumber, req.ToNumber, req.CallbackURL)
	if err != nil {
		return "", fmt.Errorf("plivo: place call: %w", err)
	}
	return model.ProviderCallID(uuid), nil
}

func (p *Plivo) Hangup(ctx context.Context, id model.ProviderCallID) error {
	if err := p.client.HangupCall(ctx, p.authID, p.authToken, string(id)); err != nil {
		return fmt.Errorf("plivo: hangup: %w", err)
	}
	return nil
}

func (p *Plivo) TransferToHuman(ctx context.Context, id model.ProviderCallID, targetNumber string) error {
	if err := p.client.TransferCall(ctx, p.authID, p.authToken, string(id), targetNumber); err != nil {
		return fmt.Errorf("plivo: transfer: %w", err)
	}
	return nil
}

func (p *Plivo) GetRecording(ctx context.Context, id model.ProviderCallID) (model.RecordingRef, error) {
	url, err := p.client.GetRecordingURL(ctx, p.authID, p.authToken, string(id))
	if err != nil {
		return model.RecordingRef{}, fmt.Errorf("plivo: get recording: %w", err)
	}
	return model.RecordingRef{
		RecordingID: fmt.Sprintf("plivo-%s", id),
		StorageKey:  url,
		Encrypted:   false, // raw provider URL; caller uploads to DO Spaces with encryption
	}, nil
}
