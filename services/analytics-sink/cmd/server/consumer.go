package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/sink"
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

	if writer, ok := svc.Store().(sink.Writer); ok {
		startBatchConsumers(ctx, log, p, sink.New(writer))
		return
	}
	startLegacyConsumers(ctx, log, p, svc)
}

func firehoseConfigs() []events.ConsumerConfig {
	return []events.ConsumerConfig{
		{Stream: "CAPSY_CALL", Durable: "analytics-sink-call", FilterSubject: "call.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_LEAD", Durable: "analytics-sink-lead", FilterSubject: "lead.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_HANDOFF", Durable: "analytics-sink-handoff", FilterSubject: "handoff.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_WA", Durable: "analytics-sink-wa", FilterSubject: "wa.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_BILLING", Durable: "analytics-sink-billing", FilterSubject: "billing.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_CAMPAIGN", Durable: "analytics-sink-campaign", FilterSubject: "campaign.>", DeliverPolicy: jetstream.DeliverAllPolicy},
		{Stream: "CAPSY_SITE", Durable: "analytics-sink-site", FilterSubject: "site.>", DeliverPolicy: jetstream.DeliverAllPolicy},
	}
}

func startBatchConsumers(ctx context.Context, log *slog.Logger, p *events.JetStream, analytics *sink.Sink) {
	for _, cfg := range firehoseConfigs() {
		cfg := cfg
		go runBatchConsumer(ctx, log, p, cfg, analytics)
	}
	<-ctx.Done()
}

func runBatchConsumer(ctx context.Context, log *slog.Logger, p *events.JetStream, cfg events.ConsumerConfig, analytics *sink.Sink) {
	stream, err := p.JS().Stream(ctx, cfg.Stream)
	if err != nil {
		log.Error("analytics-sink: stream lookup", "stream", cfg.Stream, "error", err)
		return
	}
	if cfg.AckWait == 0 {
		cfg.AckWait = 30 * time.Second
	}
	if cfg.MaxDeliver == 0 {
		cfg.MaxDeliver = 5
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       cfg.Durable,
		FilterSubject: cfg.FilterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: cfg.DeliverPolicy,
		AckWait:       cfg.AckWait,
		MaxDeliver:    cfg.MaxDeliver,
	})
	if err != nil {
		log.Error("analytics-sink: consumer create", "durable", cfg.Durable, "error", err)
		return
	}

	for ctx.Err() == nil {
		batch, err := consumer.Fetch(sink.DefaultBatchSize, jetstream.FetchMaxWait(sink.DefaultFlushInterval))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("analytics-sink: fetch", "durable", cfg.Durable, "error", err)
			time.Sleep(time.Second)
			continue
		}
		var msgs []jetstream.Msg
		var rows []model.FactRow
		for msg := range batch.Messages() {
			event, err := eventFromMessage(msg)
			if err != nil {
				log.Warn("analytics-sink: parse event", "subject", msg.Subject(), "error", err)
				_ = msg.Ack()
				continue
			}
			row, err := analytics.EventToRow(event)
			if err != nil {
				if strings.Contains(err.Error(), "unsupported event type") {
					_ = msg.Ack()
					continue
				}
				log.Warn("analytics-sink: transform event", "subject", msg.Subject(), "error", err)
				_ = msg.Ack()
				continue
			}
			msgs = append(msgs, msg)
			rows = append(rows, row)
		}
		if err := batch.Error(); err != nil && ctx.Err() == nil {
			log.Warn("analytics-sink: fetch batch ended with error", "durable", cfg.Durable, "error", err)
		}
		if len(rows) == 0 {
			continue
		}
		if err := analytics.WriteRows(ctx, rows); err != nil {
			log.Error("analytics-sink: clickhouse batch write", "durable", cfg.Durable, "rows", len(rows), "error", err)
			for _, msg := range msgs {
				_ = msg.Nak()
			}
			continue
		}
		for _, msg := range msgs {
			_ = msg.Ack()
		}
		log.Debug("analytics-sink: batch flushed", "durable", cfg.Durable, "rows", len(rows))
	}
}

func startLegacyConsumers(ctx context.Context, log *slog.Logger, p *events.JetStream, svc *service.Service) {
	for _, cfg := range firehoseConfigs() {
		cfg := cfg
		go func() {
			_ = p.Subscribe(ctx, cfg, func(ctx context.Context, subject string, data []byte, hdrs map[string][]string) error {
				return ingest(ctx, log, svc, subject, data, hdrs)
			})
		}()
	}
	<-ctx.Done()
}

func eventFromMessage(msg jetstream.Msg) (model.CanonicalEvent, error) {
	var raw map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(msg.Data())))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return model.CanonicalEvent{}, err
	}
	eventID := msg.Headers().Get("Nats-Msg-Id")
	if eventID == "" {
		eventID = stringVal(raw, "event_id", "session_id", "call_id", "turn_id", "message_id", "wa_message_id", "handoff_id", "visit_id", "cost_event_id", "lead_id", "output_id", "retrieval_id", "id")
	}
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%d", msg.Subject(), time.Now().UnixNano())
	}
	occurredAt := timeVal(raw, "occurred_at", "ended_at", "created_at", "sent_at", "timestamp", "ts")
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return model.CanonicalEvent{
		EventID:    eventID,
		TenantID:   fallbackString(stringVal(raw, "tenant_id"), "unknown"),
		CampaignID: stringVal(raw, "campaign_id"),
		ProjectID:  stringVal(raw, "project_id"),
		UserID:     stringVal(raw, "user_id", "assigned_user_id", "salesperson_id"),
		Type:       msg.Subject(),
		Payload:    raw,
		OccurredAt: occurredAt,
	}, nil
}

func ingest(ctx context.Context, log *slog.Logger, svc *service.Service, subject string, data []byte, hdrs map[string][]string) error {
	var raw map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		log.Warn("analytics-sink: parse event", "subject", subject, "error", err)
		return nil
	}

	eventID := stringVal(raw, "event_id", "session_id", "call_id", "turn_id", "id")
	if len(hdrs["Nats-Msg-Id"]) > 0 {
		eventID = hdrs["Nats-Msg-Id"][0]
	}
	if eventID == "" {
		eventID = subject + "-" + time.Now().Format(time.RFC3339Nano)
	}

	ev := model.CanonicalEvent{
		EventID:    eventID,
		TenantID:   fallbackString(stringVal(raw, "tenant_id"), "unknown"),
		CampaignID: stringVal(raw, "campaign_id"),
		ProjectID:  stringVal(raw, "project_id"),
		UserID:     stringVal(raw, "user_id", "assigned_user_id", "salesperson_id"),
		Type:       subject,
		Payload:    raw,
		OccurredAt: timeVal(raw, "occurred_at", "ended_at", "created_at", "sent_at", "timestamp", "ts"),
	}

	if _, err := svc.Ingest(ctx, ev); err != nil {
		if strings.Contains(err.Error(), "unsupported event type") {
			return nil
		}
		log.Error("analytics-sink: ingest", "subject", subject, "error", err)
		return err
	}
	return nil
}

func stringVal(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch value := v.(type) {
			case string:
				if value != "" {
					return value
				}
			case json.Number:
				return value.String()
			case float64:
				return strconv.FormatFloat(value, 'f', -1, 64)
			}
		}
	}
	return ""
}

func timeVal(m map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch value := v.(type) {
		case string:
			if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return ts.UTC()
			}
		case json.Number:
			if ts, ok := unixTime(value.String()); ok {
				return ts
			}
		case float64:
			if ts, ok := unixTime(strconv.FormatFloat(value, 'f', -1, 64)); ok {
				return ts
			}
		}
	}
	return time.Time{}
}

func unixTime(raw string) (time.Time, bool) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return time.Time{}, false
	}
	if value > 1_000_000_000_000 {
		return time.UnixMilli(int64(value)).UTC(), true
	}
	return time.Unix(int64(value), 0).UTC(), true
}

func fallbackString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
