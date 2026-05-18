//go:build integration && nats && clickhouse

package integration

import (
	"context"
	"testing"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestJetStreamToClickHouseContractWithFakes(t *testing.T) {
	ctx := context.Background()
	svc := service.New(store.NewFake())
	created, err := svc.Ingest(ctx, model.CanonicalEvent{
		EventID:   "evt-js-1",
		TenantID:  "tenant-1",
		Type:      "site_visit.completed",
		ProjectID: "project-1",
	})
	if err != nil || !created {
		t.Fatalf("ingest created=%v err=%v", created, err)
	}
	report, err := svc.Report(ctx, "tenant-1", "site-visit")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 || report.Rows[0].Dimension != "project-1" {
		t.Fatalf("unexpected report: %#v", report)
	}
}
