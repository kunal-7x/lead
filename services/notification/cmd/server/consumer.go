package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
)

// leadEvent is the minimal shape of lead.hot.detected and handoff.requested payloads.
type leadEvent struct {
	LeadID     string `json:"lead_id"`
	TenantID   string `json:"tenant_id"`
	CampaignID string `json:"campaign_id"`
	Recipient  string `json:"recipient"`
	AgentEmail string `json:"agent_email"`
	Reason     string `json:"reason"`
}

// startConsumer subscribes to notification-relevant NATS subjects. No-op when NATS_URL unset.
func startConsumer(ctx context.Context, log *slog.Logger, svc *service.Service) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		return
	}
	p, err := events.New(natsURL)
	if err != nil {
		log.Error("notification: nats connect", "error", err)
		return
	}
	defer p.Close()
	log.Info("notification: JetStream consumer started", "url", natsURL)

	go func() {
		_ = p.Subscribe(ctx, events.ConsumerConfig{
			Stream:        "CAPSY_LEAD",
			Durable:       "notification-lead-hot",
			FilterSubject: events.SubjectLeadHotDetected,
		}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
			var ev leadEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				log.Warn("notification: parse lead.hot.detected", "error", err)
				return nil
			}
			if ev.TenantID == "" || ev.Recipient == "" {
				return nil
			}
			_, err := svc.Send(ctx, model.SendRequest{
				TenantID:  ev.TenantID,
				Channel:   model.ChannelDashboard,
				Recipient: ev.Recipient,
				Template:  "lead_hot_detected",
				Payload:   map[string]any{"lead_id": ev.LeadID, "campaign_id": ev.CampaignID},
			})
			if err != nil {
				log.Error("notification: send lead_hot_detected", "error", err)
				return err
			}
			return nil
		})
	}()

	go func() {
		_ = p.Subscribe(ctx, events.ConsumerConfig{
			Stream:        "CAPSY_HANDOFF",
			Durable:       "notification-handoff-requested",
			FilterSubject: events.SubjectHandoffRequested,
		}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
			var ev leadEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				log.Warn("notification: parse handoff.requested", "error", err)
				return nil
			}
			if ev.TenantID == "" {
				return nil
			}
			recipient := ev.AgentEmail
			if recipient == "" {
				recipient = ev.Recipient
			}
			if recipient == "" {
				return nil
			}
			_, err := svc.Send(ctx, model.SendRequest{
				TenantID:  ev.TenantID,
				Channel:   model.ChannelDashboard,
				Recipient: recipient,
				Template:  "handoff_requested",
				Payload:   map[string]any{"lead_id": ev.LeadID, "reason": ev.Reason},
			})
			if err != nil {
				log.Error("notification: send handoff_requested", "error", err)
				return err
			}
			return nil
		})
	}()

	<-ctx.Done()
}
