package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/site-visit/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_site_visit")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) SaveVisit(ctx context.Context, visit model.Visit) (model.Visit, error) {
	now := time.Now().UTC()
	if visit.ID == "" {
		visit.ID = pgkv.NewID("visit")
	}
	if visit.CreatedAt.IsZero() {
		visit.CreatedAt = now
	}
	visit.UpdatedAt = now
	return visit, p.kv.Put(ctx, "visits", visit.ID, visit)
}

func (p *PostgresStore) GetVisit(ctx context.Context, id string) (model.Visit, error) {
	visit, ok, err := pgkv.Get[model.Visit](ctx, p.kv, "visits", id)
	if err != nil {
		return model.Visit{}, err
	}
	if !ok {
		return model.Visit{}, fmt.Errorf("visit %q: %w", id, ErrNotFound)
	}
	return visit, nil
}

func (p *PostgresStore) ListVisits(ctx context.Context, tenantID string) ([]model.Visit, error) {
	items, err := pgkv.List[model.Visit](ctx, p.kv, "visits")
	if err != nil {
		return nil, err
	}
	out := make([]model.Visit, 0, len(items))
	for _, item := range items {
		if tenantID == "" || item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *PostgresStore) AddEvent(ctx context.Context, event model.Event) (model.Event, error) {
	if event.ID == "" {
		event.ID = pgkv.NewID("event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, p.kv.Put(ctx, "events", event.ID, event)
}

func (p *PostgresStore) ListEvents(ctx context.Context, visitID string) ([]model.Event, error) {
	items, err := pgkv.List[model.Event](ctx, p.kv, "events")
	if err != nil {
		return nil, err
	}
	out := make([]model.Event, 0, len(items))
	for _, item := range items {
		if visitID == "" || item.VisitID == visitID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *PostgresStore) SaveWorkflowAction(ctx context.Context, action model.WorkflowAction) (model.WorkflowAction, error) {
	if action.ID == "" {
		action.ID = pgkv.NewID("workflow")
	}
	if action.CreatedAt.IsZero() {
		action.CreatedAt = time.Now().UTC()
	}
	return action, p.kv.Put(ctx, "workflow_actions", action.ID, action)
}

func (p *PostgresStore) ListWorkflowActions(ctx context.Context, tenantID string) ([]model.WorkflowAction, error) {
	items, err := pgkv.List[model.WorkflowAction](ctx, p.kv, "workflow_actions")
	if err != nil {
		return nil, err
	}
	out := make([]model.WorkflowAction, 0, len(items))
	for _, item := range items {
		if tenantID == "" || item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *PostgresStore) MarkWorkflowActionFired(ctx context.Context, id, firedBy string) (model.WorkflowAction, error) {
	action, ok, err := pgkv.Get[model.WorkflowAction](ctx, p.kv, "workflow_actions", id)
	if err != nil {
		return model.WorkflowAction{}, err
	}
	if !ok {
		return model.WorkflowAction{}, fmt.Errorf("workflow action %q: %w", id, ErrNotFound)
	}
	action.FiredAt = time.Now().UTC()
	return action, p.kv.Put(ctx, "workflow_actions", id, action)
}
