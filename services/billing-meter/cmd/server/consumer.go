package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/service"
)

// startConsumer connects to JetStream and subscribes to billing-relevant subjects.
// No-op if NATS_URL is not set. Blocks until ctx is cancelled.
func startConsumer(ctx context.Context, log *slog.Logger, svc *service.Service) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		return
	}
	p, err := events.New(natsURL)
	if err != nil {
		log.Error("billing-meter: nats connect", "error", err)
		return
	}
	defer p.Close()
	log.Info("billing-meter: JetStream consumer started", "url", natsURL)

	go func() {
		_ = p.Subscribe(ctx, events.ConsumerConfig{
			Stream:        "CAPSY_CALL",
			Durable:       "billing-meter-call-completed",
			FilterSubject: events.SubjectCallCompleted,
		}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
			return handleUsage(ctx, log, svc, subject, data, model.UsageCallCompleted)
		})
	}()

	go func() {
		_ = p.Subscribe(ctx, events.ConsumerConfig{
			Stream:        "CAPSY_WA",
			Durable:       "billing-meter-wa-template-sent",
			FilterSubject: events.SubjectWATemplateSent,
		}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
			return handleUsage(ctx, log, svc, subject, data, model.UsageWhatsApp)
		})
	}()

	<-ctx.Done()
}

// callEvent is the minimal subset parsed from call.completed messages.
type callEvent struct {
	SessionID  string    `json:"session_id"`
	TenantID   string    `json:"tenant_id"`
	CampaignID string    `json:"campaign_id"`
	Provider   string    `json:"provider"`
	DurationS  int64     `json:"duration_seconds"`
	EndedAt    time.Time `json:"ended_at"`
}

func handleUsage(ctx context.Context, log *slog.Logger, svc *service.Service, subject string, data []byte, usageType model.UsageType) error {
	var ev callEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		log.Warn("billing-meter: parse event", "subject", subject, "error", err)
		return nil // don't redeliver unparseable events
	}
	usage := model.UsageEvent{
		ID:         ev.SessionID,
		TenantID:   ev.TenantID,
		CampaignID: ev.CampaignID,
		Type:       usageType,
		Quantity:   ev.DurationS,
		Unit:       "seconds",
		Provider:   ev.Provider,
		OccurredAt: ev.EndedAt,
	}
	if usage.TenantID == "" {
		return nil
	}
	if _, err := svc.RecordUsage(ctx, usage); err != nil {
		log.Error("billing-meter: record usage", "subject", subject, "error", err)
		return err
	}
	return nil
}
