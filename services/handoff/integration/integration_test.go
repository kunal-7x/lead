//go:build integration && temporal

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/handoff/internal/model"
	"github.com/lead/services/handoff/internal/service"
	"github.com/lead/services/handoff/internal/store"
)

func TestTemporalReplayEscalationCascade(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	start := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	svc.SetNow(func() time.Time { return start })
	_ = st.SaveSalesperson(ctx, model.Salesperson{ID: "rep-1", TenantID: "tenant-1", TeamID: "team-1", ManagerID: "manager-1", PerformanceScore: 9, MaxOpenTasks: 10, Active: true})
	_ = st.SaveScoringSnapshot(ctx, model.ScoringSnapshot{ID: "score-1", TenantID: "tenant-1", LeadID: "lead-1", TeamID: "team-1", Temperature: model.TemperatureHot, BuyerType: model.BuyerTypeSelfUse})
	handoff, err := svc.CreateHandoff(ctx, model.CreateHandoffRequest{TenantID: "tenant-1", LeadID: "lead-1", Reason: "hot_lead", Summary: "hot", ScoringSnapshotID: "score-1"})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if err := svc.EvaluateSLAs(ctx, start.Add(21*time.Minute)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	events, _ := st.ListSLAEvents(ctx, "tenant-1")
	stages := map[string]bool{}
	for _, event := range events {
		if event.HandoffID == handoff.ID {
			stages[event.Stage+":"+event.Type] = true
		}
	}
	for _, key := range []string{"rep:sla_missed", "manager:escalated", "manager:sla_missed", "tenant_owner:alerted"} {
		if !stages[key] {
			t.Fatalf("missing cascade event %s in %#v", key, stages)
		}
	}
}
