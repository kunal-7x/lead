package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lead/services/site-visit/internal/model"
)

var ErrNotFound = errors.New("not found")

type Fake struct {
	mu      sync.Mutex
	seq     int
	visits  map[string]*model.Visit
	events  []*model.Event
	actions map[string]*model.WorkflowAction
}

func NewFake() *Fake {
	return &Fake{
		visits:  make(map[string]*model.Visit),
		actions: make(map[string]*model.WorkflowAction),
	}
}

func (f *Fake) SaveVisit(_ context.Context, visit model.Visit) (model.Visit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	if visit.ID == "" {
		visit.ID = f.nextID("visit")
	}
	if visit.CreatedAt.IsZero() {
		visit.CreatedAt = now
	}
	visit.UpdatedAt = now
	cp := cloneVisit(visit)
	f.visits[visit.ID] = &cp
	return visit, nil
}

func (f *Fake) GetVisit(_ context.Context, id string) (model.Visit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	visit, ok := f.visits[id]
	if !ok {
		return model.Visit{}, fmt.Errorf("visit %q: %w", id, ErrNotFound)
	}
	return cloneVisit(*visit), nil
}

func (f *Fake) ListVisits(_ context.Context, tenantID string) ([]model.Visit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Visit, 0, len(f.visits))
	for _, visit := range f.visits {
		if tenantID == "" || visit.TenantID == tenantID {
			out = append(out, cloneVisit(*visit))
		}
	}
	return out, nil
}

func (f *Fake) AddEvent(_ context.Context, event model.Event) (model.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.ID == "" {
		event.ID = f.nextID("event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	cp := cloneEvent(event)
	f.events = append(f.events, &cp)
	return event, nil
}

func (f *Fake) ListEvents(_ context.Context, visitID string) ([]model.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Event, 0, len(f.events))
	for _, event := range f.events {
		if visitID == "" || event.VisitID == visitID {
			out = append(out, cloneEvent(*event))
		}
	}
	return out, nil
}

func (f *Fake) SaveWorkflowAction(_ context.Context, action model.WorkflowAction) (model.WorkflowAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if action.ID == "" {
		action.ID = f.nextID("workflow")
	}
	if action.CreatedAt.IsZero() {
		action.CreatedAt = time.Now().UTC()
	}
	cp := action
	f.actions[action.ID] = &cp
	return action, nil
}

func (f *Fake) ListWorkflowActions(_ context.Context, tenantID string) ([]model.WorkflowAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.WorkflowAction, 0, len(f.actions))
	for _, action := range f.actions {
		if tenantID == "" || action.TenantID == tenantID {
			out = append(out, *action)
		}
	}
	return out, nil
}

func (f *Fake) MarkWorkflowActionFired(_ context.Context, id, _ string) (model.WorkflowAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	action, ok := f.actions[id]
	if !ok {
		return model.WorkflowAction{}, fmt.Errorf("workflow action %q: %w", id, ErrNotFound)
	}
	action.FiredAt = time.Now().UTC()
	return *action, nil
}

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%06d", prefix, f.seq)
}

func cloneVisit(v model.Visit) model.Visit {
	v.ProposedSlots = append([]model.ProposedSlot(nil), v.ProposedSlots...)
	return v
}

func cloneEvent(e model.Event) model.Event {
	if e.Payload != nil {
		payload := make(map[string]any, len(e.Payload))
		for k, v := range e.Payload {
			payload[k] = v
		}
		e.Payload = payload
	}
	return e
}
