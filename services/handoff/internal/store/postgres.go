package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/handoff/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_handoff")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) SaveSalesperson(ctx context.Context, rep model.Salesperson) error {
	if rep.ID == "" {
		rep.ID = pgkv.NewID("rep")
	}
	if rep.MaxOpenTasks == 0 {
		rep.MaxOpenTasks = 100
	}
	return p.kv.Put(ctx, "salespeople", rep.ID, rep)
}

func (p *PostgresStore) ListSalespeople(ctx context.Context, tenantID, teamID string) ([]model.Salesperson, error) {
	reps, err := pgkv.List[model.Salesperson](ctx, p.kv, "salespeople")
	if err != nil {
		return nil, err
	}
	out := make([]model.Salesperson, 0, len(reps))
	for _, rep := range reps {
		if rep.TenantID == tenantID && rep.TeamID == teamID && rep.Active {
			out = append(out, rep)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (p *PostgresStore) CountOpenTasks(ctx context.Context, userID string) (int, error) {
	tasks, err := pgkv.List[model.Task](ctx, p.kv, "tasks")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, task := range tasks {
		if task.UserID == userID && task.Status == model.TaskStatusOpen {
			count++
		}
	}
	return count, nil
}

func (p *PostgresStore) SaveScoringSnapshot(ctx context.Context, snapshot model.ScoringSnapshot) error {
	if snapshot.ID == "" {
		snapshot.ID = pgkv.NewID("score")
	}
	return p.kv.Put(ctx, "scoring_snapshots", snapshot.ID, snapshot)
}

func (p *PostgresStore) GetScoringSnapshot(ctx context.Context, id string) (model.ScoringSnapshot, error) {
	snapshot, ok, err := pgkv.Get[model.ScoringSnapshot](ctx, p.kv, "scoring_snapshots", id)
	if err != nil {
		return model.ScoringSnapshot{}, err
	}
	if !ok {
		return model.ScoringSnapshot{}, fmt.Errorf("scoring snapshot %q: %w", id, ErrNotFound)
	}
	return snapshot, nil
}

func (p *PostgresStore) SaveHandoff(ctx context.Context, handoff model.Handoff) (model.Handoff, error) {
	if handoff.ID == "" {
		handoff.ID = pgkv.NewID("handoff")
	}
	if handoff.CreatedAt.IsZero() {
		handoff.CreatedAt = time.Now().UTC()
	}
	return handoff, p.kv.Put(ctx, "handoffs", handoff.ID, handoff)
}

func (p *PostgresStore) GetHandoff(ctx context.Context, id string) (model.Handoff, error) {
	handoff, ok, err := pgkv.Get[model.Handoff](ctx, p.kv, "handoffs", id)
	if err != nil {
		return model.Handoff{}, err
	}
	if !ok {
		return model.Handoff{}, fmt.Errorf("handoff %q: %w", id, ErrNotFound)
	}
	return handoff, nil
}

func (p *PostgresStore) ListHandoffs(ctx context.Context, tenantID string) ([]model.Handoff, error) {
	handoffs, err := pgkv.List[model.Handoff](ctx, p.kv, "handoffs")
	if err != nil {
		return nil, err
	}
	out := make([]model.Handoff, 0, len(handoffs))
	for _, handoff := range handoffs {
		if tenantID == "" || handoff.TenantID == tenantID {
			out = append(out, handoff)
		}
	}
	return out, nil
}

func (p *PostgresStore) SaveLeadAssignment(ctx context.Context, assignment model.LeadAssignment) (model.LeadAssignment, error) {
	if assignment.ID == "" {
		assignment.ID = pgkv.NewID("assignment")
	}
	if assignment.AssignedAt.IsZero() {
		assignment.AssignedAt = time.Now().UTC()
	}
	return assignment, p.kv.Put(ctx, "lead_assignments", assignment.ID, assignment)
}

func (p *PostgresStore) SaveTask(ctx context.Context, task model.Task) (model.Task, error) {
	if task.ID == "" {
		task.ID = pgkv.NewID("task")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	return task, p.kv.Put(ctx, "tasks", task.ID, task)
}

func (p *PostgresStore) ListTasks(ctx context.Context, tenantID string) ([]model.Task, error) {
	tasks, err := pgkv.List[model.Task](ctx, p.kv, "tasks")
	if err != nil {
		return nil, err
	}
	out := make([]model.Task, 0, len(tasks))
	for _, task := range tasks {
		if tenantID == "" || task.TenantID == tenantID {
			out = append(out, task)
		}
	}
	return out, nil
}

func (p *PostgresStore) CloseTasksForHandoff(ctx context.Context, handoffID, userID string) error {
	tasks, err := pgkv.List[model.Task](ctx, p.kv, "tasks")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, task := range tasks {
		if task.HandoffID == handoffID && (userID == "" || task.UserID == userID) {
			task.Status = model.TaskStatusClosed
			task.ClosedAt = &now
			if err := p.kv.Put(ctx, "tasks", task.ID, task); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *PostgresStore) AddTaskEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	if event.ID == "" {
		event.ID = pgkv.NewID("task_event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, p.kv.Put(ctx, "task_events", event.ID, event)
}

func (p *PostgresStore) ListTaskEvents(ctx context.Context, tenantID string) ([]model.TaskEvent, error) {
	events, err := pgkv.List[model.TaskEvent](ctx, p.kv, "task_events")
	if err != nil {
		return nil, err
	}
	out := make([]model.TaskEvent, 0, len(events))
	for _, event := range events {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (p *PostgresStore) AddSLAEvent(ctx context.Context, event model.SLAEvent) (model.SLAEvent, error) {
	if event.ID == "" {
		event.ID = pgkv.NewID("sla_event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, p.kv.Put(ctx, "sla_events", event.ID, event)
}

func (p *PostgresStore) ListSLAEvents(ctx context.Context, tenantID string) ([]model.SLAEvent, error) {
	events, err := pgkv.List[model.SLAEvent](ctx, p.kv, "sla_events")
	if err != nil {
		return nil, err
	}
	out := make([]model.SLAEvent, 0, len(events))
	for _, event := range events {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (p *PostgresStore) HasSLAEvent(ctx context.Context, handoffID, stage, eventType string) bool {
	events, err := pgkv.List[model.SLAEvent](ctx, p.kv, "sla_events")
	if err != nil {
		return false
	}
	for _, event := range events {
		if event.HandoffID == handoffID && event.Stage == stage && event.Type == eventType {
			return true
		}
	}
	return false
}
