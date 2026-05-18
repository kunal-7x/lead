package handoff_test

import (
	"context"
	"math"
	"testing"

	"github.com/lead/services/handoff/internal/model"
	"github.com/lead/services/handoff/internal/service"
	"github.com/lead/services/handoff/internal/store"
)

func TestAssignmentFairnessForHotLeads(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	for i := 0; i < 5; i++ {
		rep := model.Salesperson{
			ID:               string(rune('a' + i)),
			TenantID:         "tenant-1",
			TeamID:           "team-1",
			ManagerID:        "manager-1",
			PerformanceScore: float64(10 - i),
			MaxOpenTasks:     1000,
			Active:           true,
		}
		if err := st.SaveSalesperson(ctx, rep); err != nil {
			t.Fatalf("rep: %v", err)
		}
	}

	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		snapshotID := "score-" + stringID(i)
		err := st.SaveScoringSnapshot(ctx, model.ScoringSnapshot{
			ID:          snapshotID,
			TenantID:    "tenant-1",
			LeadID:      "lead-" + stringID(i),
			TeamID:      "team-1",
			Temperature: model.TemperatureHot,
			BuyerType:   model.BuyerTypeSelfUse,
		})
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		handoff, err := svc.CreateHandoff(ctx, model.CreateHandoffRequest{
			TenantID:          "tenant-1",
			LeadID:            "lead-" + stringID(i),
			Reason:            "hot_lead",
			Summary:           "ready for sales",
			ScoringSnapshotID: snapshotID,
		})
		if err != nil {
			t.Fatalf("handoff %d: %v", i, err)
		}
		counts[handoff.AssignedUserID]++
	}

	expected := 1000.0 / 5.0
	for rep, count := range counts {
		variance := math.Abs(float64(count)-expected) / expected
		if variance >= 0.10 {
			t.Fatalf("rep %s variance %.2f with counts %#v", rep, variance, counts)
		}
	}
}

func TestBrokerAndFakeLeadsCreateSuppressionSuggestion(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	_ = st.SaveScoringSnapshot(ctx, model.ScoringSnapshot{
		ID:          "score-broker",
		TenantID:    "tenant-1",
		LeadID:      "lead-broker",
		TeamID:      "team-1",
		Temperature: model.TemperatureWarm,
		BuyerType:   model.BuyerTypeBroker,
	})
	handoff, err := svc.CreateHandoff(ctx, model.CreateHandoffRequest{
		TenantID:          "tenant-1",
		LeadID:            "lead-broker",
		Reason:            "broker_detected",
		Summary:           "broker language detected",
		ScoringSnapshotID: "score-broker",
	})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if handoff.Status != model.HandoffStatusSuppressionSuggested || handoff.AssignedUserID != "" {
		t.Fatalf("expected suppression suggestion without assignment, got %#v", handoff)
	}
}

func stringID(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	buf := make([]byte, 0, 4)
	for i > 0 {
		buf = append([]byte{digits[i%10]}, buf...)
		i /= 10
	}
	return string(buf)
}
