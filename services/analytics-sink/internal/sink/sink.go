package sink

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/transform"
)

const (
	DefaultBatchSize     = 500
	DefaultFlushInterval = 5 * time.Second
)

type Writer interface {
	InsertBatch(context.Context, string, []model.FactRow) error
}

type Sink struct {
	writer Writer
	now    func() time.Time
}

func New(writer Writer) *Sink {
	return &Sink{writer: writer, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Sink) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Sink) EventToRow(event model.CanonicalEvent) (model.FactRow, error) {
	if event.EventID == "" || event.TenantID == "" || event.Type == "" {
		return model.FactRow{}, fmt.Errorf("event_id, tenant_id and type are required")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = s.now()
	}
	row, err := transform.EventToFact(event)
	if err != nil {
		return model.FactRow{}, err
	}
	if row.InsertedAt.IsZero() {
		row.InsertedAt = s.now()
	}
	return row, nil
}

func (s *Sink) WriteRows(ctx context.Context, rows []model.FactRow) error {
	if len(rows) == 0 {
		return nil
	}
	byTable := make(map[string][]model.FactRow)
	for _, row := range rows {
		if row.Fact == "" {
			return fmt.Errorf("fact table is required for event %q", row.EventID)
		}
		if row.InsertedAt.IsZero() {
			row.InsertedAt = s.now()
		}
		byTable[row.Fact] = append(byTable[row.Fact], row)
	}
	for table, batch := range byTable {
		if err := s.writer.InsertBatch(ctx, table, batch); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sink) WriteEvents(ctx context.Context, events []model.CanonicalEvent) error {
	rows := make([]model.FactRow, 0, len(events))
	for _, event := range events {
		row, err := s.EventToRow(event)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	return s.WriteRows(ctx, rows)
}
