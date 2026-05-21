//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestClickHouseStoreSyntheticEvents(t *testing.T) {
	url := os.Getenv("CLICKHOUSE_URL")
	if url == "" {
		t.Skip("set CLICKHOUSE_URL to run ClickHouse integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	st, err := store.NewClickHouse(url)
	if err != nil {
		t.Fatalf("new clickhouse store: %v", err)
	}
	defer st.Close()

	svc := service.New(st)
	tenantID := "tenant-c11-test-" + time.Now().Format("20060102150405")
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	events := []model.CanonicalEvent{
		{EventID: "call-1", TenantID: tenantID, CampaignID: "campaign-1", Type: "call.completed", Payload: map[string]any{"duration_s": 42}, OccurredAt: now},
		{EventID: "turn-1", TenantID: tenantID, Type: "call.turn.scored", Payload: map[string]any{"score": 0.9}, OccurredAt: now},
		{EventID: "wa-1", TenantID: tenantID, Type: "wa.message.received", Payload: map[string]any{}, OccurredAt: now},
		{EventID: "handoff-1", TenantID: tenantID, Type: "handoff.requested", Payload: map[string]any{}, OccurredAt: now},
		{EventID: "visit-1", TenantID: tenantID, ProjectID: "project-1", Type: "site_visit.completed", Payload: map[string]any{}, OccurredAt: now},
		{EventID: "cost-1", TenantID: tenantID, Type: "billing.cost_event", Payload: map[string]any{"total_inr": 4.5, "type": "call"}, OccurredAt: now},
		{EventID: "lead-1", TenantID: tenantID, Type: "lead.status.updated", Payload: map[string]any{}, OccurredAt: now},
		{EventID: "ai-1", TenantID: tenantID, Type: "ai.output.scored", Payload: map[string]any{"score": 0.7}, OccurredAt: now},
		{EventID: "kb-1", TenantID: tenantID, Type: "kb.retrieved", Payload: map[string]any{}, OccurredAt: now},
	}
	for _, event := range events {
		if _, err := svc.Ingest(ctx, event); err != nil {
			t.Fatalf("ingest %s: %v", event.Type, err)
		}
	}
	count, err := st.CountFact(ctx, tenantID, "fact_calls")
	if err != nil {
		t.Fatalf("count fact_calls: %v", err)
	}
	if count != 1 {
		t.Fatalf("fact_calls count = %d, want 1", count)
	}
	report, err := svc.Report(ctx, tenantID, "daily")
	if err != nil {
		t.Fatalf("daily report: %v", err)
	}
	if len(report.Rows) != 1 || report.Rows[0].Count != int64(len(events)) {
		t.Fatalf("unexpected daily report: %#v", report)
	}
	var views uint64
	if err := st.Client().QueryRow(ctx, `
SELECT count()
FROM system.tables
WHERE database = 'evs'
  AND name IN ('mv_daily_report', 'mv_campaign_report', 'mv_cost_report')
`).Scan(&views); err != nil {
		t.Fatalf("verify materialized views: %v", err)
	}
	if views != 3 {
		t.Fatalf("materialized views present = %d, want 3", views)
	}
}
