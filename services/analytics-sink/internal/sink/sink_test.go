package sink

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
)

type fakeWriter struct {
	batches map[string]int
}

func (f *fakeWriter) InsertBatch(_ context.Context, table string, rows []model.FactRow) error {
	if f.batches == nil {
		f.batches = map[string]int{}
	}
	f.batches[table] += len(rows)
	return nil
}

func TestSinkWriteEventsGroupsByFactTable(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	writer := &fakeWriter{}
	s := New(writer)
	err := s.WriteEvents(context.Background(), []model.CanonicalEvent{
		{EventID: "call-1", TenantID: "tenant-1", Type: "call.completed", Payload: map[string]any{"duration_s": 30}, OccurredAt: now},
		{EventID: "wa-1", TenantID: "tenant-1", Type: "wa.message.received", Payload: map[string]any{}, OccurredAt: now},
		{EventID: "wa-2", TenantID: "tenant-1", Type: "wa.message.sent", Payload: map[string]any{}, OccurredAt: now},
	})
	if err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	if writer.batches["fact_calls"] != 1 {
		t.Fatalf("fact_calls batch count = %d", writer.batches["fact_calls"])
	}
	if writer.batches["fact_whatsapp"] != 2 {
		t.Fatalf("fact_whatsapp batch count = %d", writer.batches["fact_whatsapp"])
	}
}
