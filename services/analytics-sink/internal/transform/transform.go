package transform

import (
	"fmt"

	"github.com/lead/services/analytics-sink/internal/model"
)

func EventToFact(event model.CanonicalEvent) (model.FactRow, error) {
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
	switch event.Type {
	case "call.completed":
		fact.Fact = "fact_calls"
		fact.Metric = "call_count"
		fact.Value = number(event.Payload["duration_seconds"])
	case "call.turn.recorded":
		fact.Fact = "fact_call_turns"
		fact.Metric = "turn_count"
		fact.Value = 1
	case "whatsapp.delivered", "whatsapp.reply", "whatsapp.opt_out":
		fact.Fact = "fact_whatsapp"
		fact.Metric = "message_count"
		fact.Value = 1
	case "site_visit.created", "site_visit.completed", "site_visit.no_show":
		fact.Fact = "fact_site_visits"
		fact.Metric = "site_visit_count"
		fact.Value = 1
	case "handoff.created", "handoff.escalated", "handoff.acknowledged":
		fact.Fact = "fact_handoffs"
		fact.Metric = "handoff_count"
		fact.Value = 1
	case "billing.cost":
		fact.Fact = "fact_costs"
		fact.Metric = "cost_inr"
		fact.Value = number(event.Payload["cost_inr"])
	case "lead.status.updated":
		fact.Fact = "fact_lead_status_changes"
		fact.Metric = "status_change_count"
		fact.Value = 1
	case "llm.completion", "guardrail.checked":
		fact.Fact = "fact_ai_outputs"
		fact.Metric = "ai_output_count"
		fact.Value = numberOr(event.Payload["tokens"], 1)
	case "kb.retrieval":
		fact.Fact = "fact_kb_retrievals"
		fact.Metric = "retrieval_count"
		fact.Value = 1
	default:
		return model.FactRow{}, fmt.Errorf("unsupported event type %q", event.Type)
	}
	return fact, nil
}

func number(value any) float64 {
	return numberOr(value, 0)
}

func numberOr(value any, fallback float64) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case float32:
		return float64(v)
	default:
		return fallback
	}
}
