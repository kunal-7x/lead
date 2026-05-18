package analyticssink_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestFreshnessEventVisibleInReportUnder15Seconds(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	st.SetNow(func() time.Time { return now })
	svc := service.New(st)
	svc.SetNow(func() time.Time { return now })
	_, err := svc.Ingest(ctx, model.CanonicalEvent{
		EventID:    "evt-fresh",
		TenantID:   "tenant-1",
		CampaignID: "campaign-1",
		Type:       "whatsapp.delivered",
		OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	st.SetNow(func() time.Time { return now.Add(10 * time.Second) })
	report, err := svc.Report(ctx, "tenant-1", "campaign")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.FreshnessS >= 15 || len(report.Rows) != 1 {
		t.Fatalf("expected freshness <15s with visible row, got %#v", report)
	}
}
