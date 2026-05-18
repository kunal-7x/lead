package billingmeter_test

import (
	"context"
	"testing"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/service"
	"github.com/lead/services/billing-meter/internal/store"
)

func TestTenantCapBreachDoesNotAffectOtherTenant(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	_ = svc.SetCaps(ctx, model.Caps{TenantID: "tenant-a", MonthlyINR: 1})
	_ = svc.SetCaps(ctx, model.Caps{TenantID: "tenant-b", MonthlyINR: 1000})
	_, _ = svc.RecordUsage(ctx, model.UsageEvent{TenantID: "tenant-a", Type: model.UsageWhatsApp, Category: "marketing", Quantity: 2})
	signalsA, _ := st.ListSignals(ctx, "tenant-a")
	signalsB, _ := st.ListSignals(ctx, "tenant-b")
	if !hasTenantSignal(signalsA, "tenant.cap.reached", "tenant-a") {
		t.Fatalf("tenant A should breach cap, got %#v", signalsA)
	}
	if len(signalsB) != 0 {
		t.Fatalf("tenant B should not receive tenant A signals, got %#v", signalsB)
	}
}

func hasTenantSignal(signals []model.Signal, typ, tenantID string) bool {
	for _, signal := range signals {
		if signal.Type == typ && signal.TenantID == tenantID {
			return true
		}
	}
	return false
}
