package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lead/services/handoff/internal/model"
)

var ErrNotFound = errors.New("not found")

type Fake struct {
	mu          sync.Mutex
	seq         int
	reps        map[string]*model.Salesperson
	snapshots   map[string]*model.ScoringSnapshot
	handoffs    map[string]*model.Handoff
	assignments []*model.LeadAssignment
	tasks       map[string]*model.Task
	taskEvents  []*model.TaskEvent
	slaEvents   []*model.SLAEvent
}

func NewFake() *Fake {
	return &Fake{
		reps:      make(map[string]*model.Salesperson),
		snapshots: make(map[string]*model.ScoringSnapshot),
		handoffs:  make(map[string]*model.Handoff),
		tasks:     make(map[string]*model.Task),
	}
}

func (f *Fake) SaveSalesperson(_ context.Context, rep model.Salesperson) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rep.ID == "" {
		rep.ID = f.nextID("rep")
	}
	if rep.MaxOpenTasks == 0 {
		rep.MaxOpenTasks = 100
	}
	cp := rep
	f.reps[rep.ID] = &cp
	return nil
}

func (f *Fake) ListSalespeople(_ context.Context, tenantID, teamID string) ([]model.Salesperson, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Salesperson, 0, len(f.reps))
	for _, rep := range f.reps {
		if rep.TenantID == tenantID && rep.TeamID == teamID && rep.Active {
			out = append(out, *rep)
		}
	}
	slices.SortFunc(out, func(a, b model.Salesperson) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out, nil
}

func (f *Fake) CountOpenTasks(_ context.Context, userID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, task := range f.tasks {
		if task.UserID == userID && task.Status == model.TaskStatusOpen {
			count++
		}
	}
	return count, nil
}

func (f *Fake) SaveScoringSnapshot(_ context.Context, snapshot model.ScoringSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if snapshot.ID == "" {
		snapshot.ID = f.nextID("score")
	}
	cp := snapshot
	f.snapshots[snapshot.ID] = &cp
	return nil
}

func (f *Fake) GetScoringSnapshot(_ context.Context, id string) (model.ScoringSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot, ok := f.snapshots[id]
	if !ok {
		return model.ScoringSnapshot{}, fmt.Errorf("scoring snapshot %q: %w", id, ErrNotFound)
	}
	return *snapshot, nil
}

func (f *Fake) SaveHandoff(_ context.Context, handoff model.Handoff) (model.Handoff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if handoff.ID == "" {
		handoff.ID = f.nextID("handoff")
	}
	if handoff.CreatedAt.IsZero() {
		handoff.CreatedAt = time.Now().UTC()
	}
	cp := handoff
	f.handoffs[handoff.ID] = &cp
	return handoff, nil
}

func (f *Fake) GetHandoff(_ context.Context, id string) (model.Handoff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	handoff, ok := f.handoffs[id]
	if !ok {
		return model.Handoff{}, fmt.Errorf("handoff %q: %w", id, ErrNotFound)
	}
	return *handoff, nil
}

func (f *Fake) ListHandoffs(_ context.Context, tenantID string) ([]model.Handoff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Handoff, 0, len(f.handoffs))
	for _, handoff := range f.handoffs {
		if tenantID == "" || handoff.TenantID == tenantID {
			out = append(out, *handoff)
		}
	}
	return out, nil
}

func (f *Fake) SaveLeadAssignment(_ context.Context, assignment model.LeadAssignment) (model.LeadAssignment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if assignment.ID == "" {
		assignment.ID = f.nextID("assignment")
	}
	if assignment.AssignedAt.IsZero() {
		assignment.AssignedAt = time.Now().UTC()
	}
	cp := assignment
	f.assignments = append(f.assignments, &cp)
	return assignment, nil
}

func (f *Fake) SaveTask(_ context.Context, task model.Task) (model.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if task.ID == "" {
		task.ID = f.nextID("task")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	cp := task
	f.tasks[task.ID] = &cp
	return task, nil
}

func (f *Fake) ListTasks(_ context.Context, tenantID string) ([]model.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		if tenantID == "" || task.TenantID == tenantID {
			out = append(out, *task)
		}
	}
	return out, nil
}

func (f *Fake) CloseTasksForHandoff(_ context.Context, handoffID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for _, task := range f.tasks {
		if task.HandoffID == handoffID && (userID == "" || task.UserID == userID) {
			task.Status = model.TaskStatusClosed
			task.ClosedAt = &now
		}
	}
	return nil
}

func (f *Fake) AddTaskEvent(_ context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.ID == "" {
		event.ID = f.nextID("task_event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	cp := event
	f.taskEvents = append(f.taskEvents, &cp)
	return event, nil
}

func (f *Fake) ListTaskEvents(_ context.Context, tenantID string) ([]model.TaskEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.TaskEvent, 0, len(f.taskEvents))
	for _, event := range f.taskEvents {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, *event)
		}
	}
	return out, nil
}

func (f *Fake) AddSLAEvent(_ context.Context, event model.SLAEvent) (model.SLAEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.ID == "" {
		event.ID = f.nextID("sla_event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	cp := event
	f.slaEvents = append(f.slaEvents, &cp)
	return event, nil
}

func (f *Fake) ListSLAEvents(_ context.Context, tenantID string) ([]model.SLAEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.SLAEvent, 0, len(f.slaEvents))
	for _, event := range f.slaEvents {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, *event)
		}
	}
	return out, nil
}

func (f *Fake) HasSLAEvent(_ context.Context, handoffID, stage, eventType string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, event := range f.slaEvents {
		if event.HandoffID == handoffID && event.Stage == stage && event.Type == eventType {
			return true
		}
	}
	return false
}

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%06d", prefix, f.seq)
}
