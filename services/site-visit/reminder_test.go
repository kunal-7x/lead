//go:build temporal

package sitevisit_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/site-visit/internal/model"
	"github.com/lead/services/site-visit/internal/service"
	"github.com/lead/services/site-visit/internal/store"
)

func TestReminderCadenceFires24HoursOnceOnlyWhenConfirmed(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	slot := model.ProposedSlot{Start: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC)}
	visit, _ := svc.CreateTentative(ctx, model.CreateTentativeRequest{TenantID: "tenant-1", LeadID: "lead-1", ProjectID: "project-1", ProposedSlots: []model.ProposedSlot{slot}})

	fired, err := svc.EvaluateWorkflows(ctx, slot.Start.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("evaluate tentative: %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("tentative visit should not fire reminders, got %#v", fired)
	}

	visit, err = svc.Confirm(ctx, visit.ID, slot, "rep-1")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	fired, err = svc.EvaluateWorkflows(ctx, slot.Start.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("evaluate confirmed: %v", err)
	}
	if countType(fired, "pre_visit_whatsapp_24h") != 1 {
		t.Fatalf("expected one T-24h WhatsApp reminder, got %#v", fired)
	}
	firedAgain, err := svc.EvaluateWorkflows(ctx, slot.Start.Add(-23*time.Hour))
	if err != nil {
		t.Fatalf("evaluate again: %v", err)
	}
	if countType(firedAgain, "pre_visit_whatsapp_24h") != 0 {
		t.Fatalf("T-24h reminder fired more than once: %#v", firedAgain)
	}
	updated, _ := st.GetVisit(ctx, visit.ID)
	if updated.State != model.StateReminderSent {
		t.Fatalf("expected reminder_sent state, got %s", updated.State)
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
