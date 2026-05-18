//go:build temporal

package billingmeter_test

import (
	"context"
	"testing"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/service"
	"github.com/lead/services/billing-meter/internal/store"
)

func TestCapWatchEmitsWarningAndPauseSignals(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	if err := svc.SetCaps(ctx, model.Caps{TenantID: "tenant-1", MonthlyINR: 10}); err != nil {
		t.Fatalf("set caps: %v", err)
	}
	_, _ = svc.RecordUsage(ctx, model.UsageEvent{TenantID: "tenant-1", Type: model.UsageWhatsApp, Category: "marketing", Quantity: 11})
	signals, _ := st.ListSignals(ctx, "tenant-1")
	if !hasSignal(signals, "billing.cap.warning", "tenant-1") {
		t.Fatalf("expected 80 percent warning, got %#v", signals)
	}
	_, _ = svc.RecordUsage(ctx, model.UsageEvent{TenantID: "tenant-1", Type: model.UsageWhatsApp, Category: "marketing", Quantity: 2})
	signals, _ = st.ListSignals(ctx, "tenant-1")
	if !hasSignal(signals, "tenant.cap.reached", "tenant-1") || !hasSignal(signals, "campaign.auto_pause.requested", "tenant-1") {
		t.Fatalf("expected 100 percent cap signals, got %#v", signals)
	}
}

func hasSignal(signals []model.Signal, typ, tenantID string) bool {
	for _, signal := range signals {
		if signal.Type == typ && signal.TenantID == tenantID {
			return true
		}
	}
	return false
}
