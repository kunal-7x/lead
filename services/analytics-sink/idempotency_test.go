package analyticssink_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestIdempotencySameEventIDOnlyOneFactCall(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	event := model.CanonicalEvent{
		EventID:    "evt-1",
		TenantID:   "tenant-1",
		Type:       "call.completed",
		Payload:    map[string]any{"duration_seconds": 90},
		OccurredAt: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
	}
	created, err := svc.Ingest(ctx, event)
	if err != nil || !created {
		t.Fatalf("first ingest created=%v err=%v", created, err)
	}
	created, err = svc.Ingest(ctx, event)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if created {
		t.Fatal("duplicate event should not create a second row")
	}
	count, err := st.CountFact(ctx, "tenant-1", "fact_calls")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one fact_calls row, got %d", count)
	}
}
