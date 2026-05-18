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

func TestNoShowRecoveryCreatesThreeContactAttemptsThenLossCapture(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	svc.SetNow(func() time.Time { return now })
	slot := model.ProposedSlot{Start: now.Add(-time.Hour), End: now}
	visit, _ := svc.CreateTentative(ctx, model.CreateTentativeRequest{TenantID: "tenant-1", LeadID: "lead-1", ProjectID: "project-1", ProposedSlots: []model.ProposedSlot{slot}})
	visit, _ = svc.Confirm(ctx, visit.ID, slot, "rep-1")
	visit, err := svc.MarkNoShow(ctx, visit.ID)
	if err != nil {
		t.Fatalf("no show: %v", err)
	}
	if visit.State != model.StateNoShow {
		t.Fatalf("expected no_show, got %s", visit.State)
	}
	actions, err := st.ListWorkflowActions(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("actions: %v", err)
	}
	required := map[string]bool{
		"noshow_whatsapp_immediate": false,
		"noshow_voice_attempt_1":    false,
		"noshow_voice_attempt_2":    false,
		"noshow_voice_attempt_3":    false,
		"noshow_loss_capture":       false,
	}
	for _, action := range actions {
		if _, ok := required[action.Type]; ok {
			required[action.Type] = true
		}
	}
	for actionType, seen := range required {
		if !seen {
			t.Fatalf("missing no-show recovery action %s in %#v", actionType, actions)
		}
	}
	fired, err := svc.EvaluateWorkflows(ctx, now.Add(49*time.Hour))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if countNoShowType(fired, "noshow_voice_attempt_1") != 1 || countNoShowType(fired, "noshow_voice_attempt_2") != 1 || countNoShowType(fired, "noshow_voice_attempt_3") != 1 || countNoShowType(fired, "noshow_loss_capture") != 1 {
		t.Fatalf("expected 3 voice attempts and loss capture, got %#v", fired)
	}
}

func countNoShowType(actions []model.WorkflowAction, typ string) int {
	count := 0
	for _, action := range actions {
		if action.Type == typ {
			count++
		}
	}
	return count
}
