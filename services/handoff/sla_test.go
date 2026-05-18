//go:build temporal

package handoff_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/handoff/internal/model"
	"github.com/lead/services/handoff/internal/service"
	"github.com/lead/services/handoff/internal/store"
)

func TestHotHandoffSLAEscalatesAfterFiveMinutes(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	start := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	svc.SetNow(func() time.Time { return start })
	_ = st.SaveSalesperson(ctx, model.Salesperson{
		ID:               "rep-1",
		TenantID:         "tenant-1",
		TeamID:           "team-1",
		ManagerID:        "manager-1",
		PerformanceScore: 10,
		MaxOpenTasks:     10,
		Active:           true,
	})
	_ = st.SaveScoringSnapshot(ctx, model.ScoringSnapshot{
		ID:          "score-hot",
		TenantID:    "tenant-1",
		LeadID:      "lead-hot",
		TeamID:      "team-1",
		Temperature: model.TemperatureHot,
		BuyerType:   model.BuyerTypeSelfUse,
	})
	handoff, err := svc.CreateHandoff(ctx, model.CreateHandoffRequest{
		TenantID:          "tenant-1",
		LeadID:            "lead-hot",
		Reason:            "hot_lead",
		Summary:           "wants immediate callback",
		ScoringSnapshotID: "score-hot",
	})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if err := svc.EvaluateSLAs(ctx, start.Add(5*time.Minute+time.Second)); err != nil {
		t.Fatalf("sla evaluate: %v", err)
	}
	updated, err := st.GetHandoff(ctx, handoff.ID)
	if err != nil {
		t.Fatalf("handoff lookup: %v", err)
	}
	if updated.Status != model.HandoffStatusEscalated {
		t.Fatalf("expected escalated handoff, got %#v", updated)
	}
	events, _ := st.ListSLAEvents(ctx, "tenant-1")
	foundMiss := false
	foundEscalation := false
	for _, event := range events {
		if event.HandoffID == handoff.ID && event.Stage == "rep" && event.Type == "sla_missed" {
			foundMiss = true
		}
		if event.HandoffID == handoff.ID && event.Stage == "manager" && event.Type == "escalated" {
			foundEscalation = true
		}
	}
	if !foundMiss || !foundEscalation {
		t.Fatalf("expected rep miss and manager escalation events, got %#v", events)
	}
}
