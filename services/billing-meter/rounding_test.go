package billingmeter_test

import (
	"context"
	"testing"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/pricing"
	"github.com/lead/services/billing-meter/internal/service"
	"github.com/lead/services/billing-meter/internal/store"
)

func TestPlivoRoundingBillsNinetySecondCallAsTwoMinutes(t *testing.T) {
	svc := service.New(store.NewFake())
	cost, err := svc.RecordUsage(context.Background(), model.UsageEvent{
		TenantID:   "tenant-1",
		CampaignID: "campaign-1",
		Type:       model.UsageCallCompleted,
		Quantity:   90,
		Unit:       "second",
	})
	if err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if cost.Quantity != 120 {
		t.Fatalf("expected 120 billed seconds, got %d", cost.Quantity)
	}
	expected := 2 * (pricing.PlivoPerMinuteINR + pricing.AIComputePerMinuteINR + pricing.RecordingPerMinuteINR)
	if cost.TotalINR != expected {
		t.Fatalf("expected %.2f INR, got %.2f", expected, cost.TotalINR)
	}
}
