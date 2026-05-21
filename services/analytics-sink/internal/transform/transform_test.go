package transform

import (
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
)

func TestEventToFact_C11Subjects(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	tests := map[string]string{
		"call.completed":       "fact_calls",
		"call.turn.scored":     "fact_call_turns",
		"wa.message.received":  "fact_whatsapp",
		"handoff.requested":    "fact_handoffs",
		"site_visit.completed": "fact_site_visits",
		"site.visit.completed": "fact_site_visits",
		"billing.cost_event":   "fact_costs",
		"lead.status.updated":  "fact_lead_status_changes",
		"ai.output.scored":     "fact_ai_outputs",
		"kb.retrieved":         "fact_kb_retrievals",
	}
	for subject, wantTable := range tests {
		t.Run(subject, func(t *testing.T) {
			row, err := EventToFact(model.CanonicalEvent{
				EventID:    "evt-" + subject,
				TenantID:   "tenant-1",
				Type:       subject,
				Payload:    map[string]any{"duration_s": 42, "total_inr": 1.25, "score": 0.91},
				OccurredAt: now,
			})
			if err != nil {
				t.Fatalf("EventToFact: %v", err)
			}
			if row.Fact != wantTable {
				t.Fatalf("Fact = %q, want %q", row.Fact, wantTable)
			}
			if row.Version == 0 {
				t.Fatal("Version should be set")
			}
		})
	}
}

func TestEventToFact_UnsupportedSubject(t *testing.T) {
	_, err := EventToFact(model.CanonicalEvent{
		EventID:    "evt-1",
		TenantID:   "tenant-1",
		Type:       "campaign.launched",
		Payload:    map[string]any{},
		OccurredAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected unsupported subject error")
	}
}
