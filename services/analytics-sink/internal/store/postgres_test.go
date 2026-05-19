//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
)

func TestPostgresStoreAnalyticsRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	row := model.FactRow{EventID: testID("event"), TenantID: tenantID, CampaignID: "campaign-a", Fact: "lead.created", Metric: "count", Value: 1, OccurredAt: time.Now().UTC()}
	inserted, err := st.InsertFact(ctx, row)
	if err != nil || !inserted {
		t.Fatalf("insert fact inserted=%v err=%v", inserted, err)
	}
	inserted, err = st.InsertFact(ctx, row)
	if err != nil || inserted {
		t.Fatalf("duplicate fact inserted=%v err=%v", inserted, err)
	}
	count, err := st.CountFact(ctx, tenantID, "lead.created")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	report, err := st.QueryReport(ctx, tenantID, "campaign")
	if err != nil || len(report.Rows) != 1 {
		t.Fatalf("report rows=%d err=%v", len(report.Rows), err)
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
