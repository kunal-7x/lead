package transform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lead/services/analytics-sink/internal/model"
)

func EventToFact(event model.CanonicalEvent) (model.FactRow, error) {
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	fact := model.FactRow{
		EventID:    event.EventID,
		TenantID:   event.TenantID,
		CampaignID: event.CampaignID,
		ProjectID:  event.ProjectID,
		UserID:     event.UserID,
		Payload:    event.Payload,
		OccurredAt: event.OccurredAt,
		Version:    event.OccurredAt.UnixNano(),
	}
	eventType := strings.ToLower(strings.TrimSpace(event.Type))
	switch {
	case eventType == "call.completed":
		fact.Fact = "fact_calls"
		fact.Metric = "duration_s"
		fact.Value = numberFirst(event.Payload, "duration_s", "duration_seconds", "duration", "call_duration_secs")
	case eventType == "call.turn.scored" || eventType == "call.turn.recorded":
		fact.Fact = "fact_call_turns"
		fact.Metric = "turn_score"
		fact.Value = numberFirstOr(event.Payload, 1, "score", "quality_score", "confidence")
	case strings.HasPrefix(eventType, "wa.message.") ||
		strings.HasPrefix(eventType, "whatsapp.") ||
		strings.HasPrefix(eventType, "wa.template.") ||
		eventType == "wa.optout":
		fact.Fact = "fact_whatsapp"
		fact.Metric = whatsappMetric(eventType)
		fact.Value = 1
	case strings.HasPrefix(eventType, "handoff."):
		fact.Fact = "fact_handoffs"
		fact.Metric = "handoff_count"
		fact.Value = 1
	case strings.HasPrefix(eventType, "site_visit.") || strings.HasPrefix(eventType, "site.visit."):
		fact.Fact = "fact_site_visits"
		fact.Metric = "site_visit_count"
		fact.Value = 1
	case eventType == "billing.cost_event" || eventType == "billing.cost" || eventType == "billing.usage":
		fact.Fact = "fact_costs"
		fact.Metric = stringFirst(event.Payload, "usage_type", "type", "unit", "category")
		if fact.Metric == "" {
			fact.Metric = "cost_inr"
		}
		fact.Value = numberFirst(event.Payload, "total_inr", "total_cost_inr", "cost_inr", "amount_inr", "value")
	case eventType == "lead.status.updated":
		fact.Fact = "fact_lead_status_changes"
		fact.Metric = "status_change_count"
		fact.Value = 1
	case eventType == "ai.output.scored" || eventType == "llm.completion" || eventType == "guardrail.checked":
		fact.Fact = "fact_ai_outputs"
		fact.Metric = "ai_output_score"
		fact.Value = numberFirstOr(event.Payload, 1, "score", "quality_score", "tokens")
	case eventType == "kb.retrieved" || eventType == "kb.retrieval":
		fact.Fact = "fact_kb_retrievals"
		fact.Metric = "retrieval_count"
		fact.Value = 1
	default:
		return model.FactRow{}, fmt.Errorf("unsupported event type %q", event.Type)
	}
	if fact.Version <= 0 {
		fact.Version = 1
	}
	return fact, nil
}

func whatsappMetric(eventType string) string {
	switch {
	case strings.Contains(eventType, "received"), strings.Contains(eventType, "reply"):
		return "message_received"
	case strings.Contains(eventType, "sent"):
		return "message_sent"
	case strings.Contains(eventType, "delivered"):
		return "message_delivered"
	case strings.Contains(eventType, "failed"):
		return "message_failed"
	case strings.Contains(eventType, "opt"):
		return "opt_out"
	default:
		return "message_count"
	}
}

func numberFirst(payload map[string]any, keys ...string) float64 {
	return numberFirstOr(payload, 0, keys...)
}

func numberFirstOr(payload map[string]any, fallback float64, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return numberOr(value, fallback)
		}
	}
	return fallback
}

func stringFirst(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if s, ok := value.(string); ok {
				return s
			}
		}
	}
	return ""
}

func number(value any) float64 {
	return numberOr(value, 0)
}

func numberOr(value any, fallback float64) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float64:
		return v
	case float32:
		return float64(v)
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		if err == nil {
			return f
		}
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return fallback
}
