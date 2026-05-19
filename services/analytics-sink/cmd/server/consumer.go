package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
)

// startConsumer subscribes to every Capsy stream (firehose) and ingests into analytics.
// No-op when NATS_URL is unset.
func startConsumer(ctx context.Context, log *slog.Logger, svc *service.Service) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		return
	}
	p, err := events.New(natsURL)
	if err != nil {
		log.Error("analytics-sink: nats connect", "error", err)
		return
	}
	defer p.Close()
	log.Info("analytics-sink: JetStream firehose consumer started", "url", natsURL)

	// firehose subjects — one per stream
	firehoseConfigs := []events.ConsumerConfig{
		{Stream: "CAPSY_CALL", Durable: "analytics-sink-call", FilterSubject: "call.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_LEAD", Durable: "analytics-sink-lead", FilterSubject: "lead.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_HANDOFF", Durable: "analytics-sink-handoff", FilterSubject: "handoff.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_WA", Durable: "analytics-sink-wa", FilterSubject: "wa.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_BILLING", Durable: "analytics-sink-billing", FilterSubject: "billing.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_CAMPAIGN", Durable: "analytics-sink-campaign", FilterSubject: "campaign.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_SITE", Durable: "analytics-sink-site", FilterSubject: "site.>", DeliverPolicy: jetstream.DeliverAllPolicy},
	}

	for _, cfg := range firehoseConfigs {
		cfg := cfg
		go func() {
			_ = p.Subscribe(ctx, cfg, func(ctx context.Context, subject string, data []byte, hdrs map[string][]string) error {
				return ingest(ctx, log, svc, subject, data, hdrs)
			})
		}()
	}

	<-ctx.Done()
}

func ingest(ctx context.Context, log *slog.Logger, svc *service.Service, subject string, data []byte, hdrs map[string][]string) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Warn("analytics-sink: parse event", "subject", subject, "error", err)
		return nil
	}

	eventID := stringVal(raw, "event_id", "session_id", "id")
	if len(hdrs["Nats-Msg-Id"]) > 0 {
		eventID = hdrs["Nats-Msg-Id"][0]
	}
	if eventID == "" {
		eventID = subject + "-" + time.Now().Format(time.RFC3339Nano)
	}

	ev := model.CanonicalEvent{
		EventID:    eventID,
		TenantID:   stringVal(raw, "tenant_id"),
		CampaignID: stringVal(raw, "campaign_id"),
		Type:       subject,
		Payload:    raw,
		OccurredAt: time.Now().UTC(),
	}
	if ev.TenantID == "" {
		ev.TenantID = "unknown"
	}

	if _, err := svc.Ingest(ctx, ev); err != nil {
		log.Error("analytics-sink: ingest", "subject", subject, "error", err)
		return err
	}
	return nil
}

func stringVal(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
