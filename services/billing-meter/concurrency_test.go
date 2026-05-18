package billingmeter_test

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/pricing"
	"github.com/lead/services/billing-meter/internal/service"
	"github.com/lead/services/billing-meter/internal/store"
)

func TestConcurrentUsageEventsKeepSummaryConsistent(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	var wg sync.WaitGroup
	for worker := 0; worker < 100; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				_, err := svc.RecordUsage(ctx, model.UsageEvent{
					TenantID:       "tenant-1",
					CampaignID:     "campaign-1",
					Type:           model.UsageWhatsApp,
					Category:       "utility",
					Quantity:       1,
					IdempotencyKey: "w-" + itoa(worker) + "-" + itoa(i),
				})
				if err != nil {
					t.Errorf("record usage: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	summary, err := st.GetSummary(ctx, "tenant", "tenant-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.UsageCount != 100000 {
		t.Fatalf("expected 100000 usage events, got %d", summary.UsageCount)
	}
	expected := 100000 * pricing.WhatsAppCategoryINR["utility"]
	if math.Abs(summary.TotalCostINR-expected) > 0.0001 {
		t.Fatalf("expected %.2f, got %.2f", expected, summary.TotalCostINR)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	const digits = "0123456789"
	buf := make([]byte, 0, 8)
	for i > 0 {
		buf = append([]byte{digits[i%10]}, buf...)
		i /= 10
	}
	return string(buf)
}
