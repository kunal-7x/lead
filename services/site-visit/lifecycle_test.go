package sitevisit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lead/services/site-visit/internal/model"
	"github.com/lead/services/site-visit/internal/service"
	"github.com/lead/services/site-visit/internal/store"
)

func TestLifecycleTransitionsAppendEventRows(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	slot := model.ProposedSlot{Start: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)}
	visit, err := svc.CreateTentative(ctx, model.CreateTentativeRequest{TenantID: "tenant-1", LeadID: "lead-1", ProjectID: "project-1", ProposedSlots: []model.ProposedSlot{slot}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	visit, err = svc.Confirm(ctx, visit.ID, slot, "rep-1")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	rescheduledSlot := model.ProposedSlot{Start: slot.Start.Add(24 * time.Hour), End: slot.End.Add(24 * time.Hour)}
	visit, err = svc.Reschedule(ctx, visit.ID, rescheduledSlot, "buyer requested")
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	visit, err = svc.Confirm(ctx, visit.ID, rescheduledSlot, "rep-1")
	if err != nil {
		t.Fatalf("confirm after reschedule: %v", err)
	}
	visit, err = svc.MarkCompleted(ctx, visit.ID, "visited sample flat", 2)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if visit.State != model.StateCompleted {
		t.Fatalf("expected completed, got %s", visit.State)
	}
	events, err := st.ListEvents(ctx, visit.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("expected one event per transition, got %d: %#v", len(events), events)
	}
	for _, event := range events {
		if event.VisitID != visit.ID || event.Type == "" {
			t.Fatalf("bad event row: %#v", event)
		}
	}
}

func TestInvalidTransitionReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := service.New(store.NewFake())
	slot := model.ProposedSlot{Start: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)}
	visit, _ := svc.CreateTentative(ctx, model.CreateTentativeRequest{TenantID: "tenant-1", LeadID: "lead-1", ProjectID: "project-1", ProposedSlots: []model.ProposedSlot{slot}})
	_, err := svc.MarkCompleted(ctx, visit.ID, "too early", 1)
	if !errors.Is(err, service.ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}
