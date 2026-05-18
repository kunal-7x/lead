//go:build integration && temporal

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/site-visit/internal/model"
	"github.com/lead/services/site-visit/internal/service"
	"github.com/lead/services/site-visit/internal/store"
)

func TestReminderAndFollowupReplay(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	slot := model.ProposedSlot{Start: time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC)}
	visit, _ := svc.CreateTentative(ctx, model.CreateTentativeRequest{TenantID: "tenant-1", LeadID: "lead-1", ProjectID: "project-1", ProposedSlots: []model.ProposedSlot{slot}})
	visit, _ = svc.Confirm(ctx, visit.ID, slot, "rep-1")
	fired, err := svc.EvaluateWorkflows(ctx, slot.Start.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("pre visit replay: %v", err)
	}
	if len(fired) < 4 {
		t.Fatalf("expected pre-visit reminders, got %#v", fired)
	}
	_, _ = svc.MarkCompleted(ctx, visit.ID, "done", 2)
	fired, err = svc.EvaluateWorkflows(ctx, slot.Start.Add(8*24*time.Hour))
	if err != nil {
		t.Fatalf("post visit replay: %v", err)
	}
	if countType(fired, "post_visit_followup_7d") != 1 {
		t.Fatalf("expected T+7d follow-up, got %#v", fired)
	}
}

func countType(actions []model.WorkflowAction, typ string) int {
	count := 0
	for _, action := range actions {
		if action.Type == typ {
			count++
		}
	}
	return count
}
