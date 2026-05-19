//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/billing-meter/internal/model"
)

func TestPostgresStoreBillingRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	usage := model.UsageEvent{TenantID: tenantID, CampaignID: testID("campaign"), Type: model.UsageLLM, Quantity: 1, Unit: "request", IdempotencyKey: testID("idem")}
	cost := model.CostEvent{TenantID: tenantID, CampaignID: usage.CampaignID, Type: usage.Type, Quantity: 1, Unit: "request", UnitCostINR: 2, TotalINR: 2}
	inserted, err := st.RecordUsageCost(ctx, usage, cost)
	if err != nil || !inserted {
		t.Fatalf("record usage inserted=%v err=%v", inserted, err)
	}
	inserted, err = st.RecordUsageCost(ctx, usage, cost)
	if err != nil || inserted {
		t.Fatalf("duplicate usage inserted=%v err=%v", inserted, err)
	}
	summary, err := st.GetSummary(ctx, "tenant", tenantID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.UsageCount != 1 || summary.TotalCostINR != 2 {
		t.Fatalf("summary = %#v", summary)
	}
}

func newTestPostgres(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	st, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	return st
}

func testID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
