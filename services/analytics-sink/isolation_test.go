package analyticssink_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestReportsAreFilteredByTenantID(t *testing.T) {
	ctx := context.Background()
	svc := service.New(store.NewFake())
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		_, err := svc.Ingest(ctx, model.CanonicalEvent{
			EventID:    "evt-" + tenant,
			TenantID:   tenant,
			Type:       "billing.cost",
			Payload:    map[string]any{"cost_inr": 100},
			OccurredAt: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("ingest %s: %v", tenant, err)
		}
	}
	report, err := svc.Report(ctx, "tenant-a", "cost")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 || report.Rows[0].Value != 100 {
		t.Fatalf("tenant-a report leaked or missed data: %#v", report)
	}
}
