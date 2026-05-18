//go:build integration && temporal

package integration

import (
	"context"
	"testing"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/service"
	"github.com/lead/services/billing-meter/internal/store"
)

func TestCampaignCapWatchEmitsPauseSignal(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	_ = svc.SetCaps(ctx, model.Caps{TenantID: "tenant-1", CampaignID: "campaign-1", MonthlyINR: 1})
	_, err := svc.RecordUsage(ctx, model.UsageEvent{TenantID: "tenant-1", CampaignID: "campaign-1", Type: model.UsageWhatsApp, Category: "marketing", Quantity: 2})
	if err != nil {
		t.Fatalf("record usage: %v", err)
	}
	signals, _ := st.ListSignals(ctx, "tenant-1")
	found := false
	for _, signal := range signals {
		if signal.CampaignID == "campaign-1" && signal.Type == "campaign.auto_pause.requested" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected campaign pause signal, got %#v", signals)
	}
}
