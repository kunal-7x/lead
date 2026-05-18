package service

import (
	"context"
	"errors"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/store"
	"github.com/lead/services/analytics-sink/internal/transform"
)

type Service struct {
	store store.Store
	now   func() time.Time
}

func New(st store.Store) *Service {
	return &Service{store: st, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) Ingest(ctx context.Context, event model.CanonicalEvent) (bool, error) {
	if event.EventID == "" || event.TenantID == "" || event.Type == "" {
		return false, errors.New("event_id, tenant_id and type are required")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = s.now()
	}
	row, err := transform.EventToFact(event)
	if err != nil {
		return false, err
	}
	row.InsertedAt = s.now()
	return s.store.InsertFact(ctx, row)
}

func (s *Service) Report(ctx context.Context, tenantID, report string) (model.ReportResponse, error) {
	return s.store.QueryReport(ctx, tenantID, report)
}

func (s *Service) Store() store.Store {
	return s.store
}
