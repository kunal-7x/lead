//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/campaign/internal/model"
)

func TestPostgresStoreCampaignRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	campaign := &model.Campaign{Name: "C1 " + testID("campaign"), TenantID: testID("tenant"), ProjectID: testID("project")}
	if err := st.CreateCampaign(ctx, campaign); err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	if err := st.SetLimits(ctx, &model.CampaignLimits{CampaignID: campaign.ID, DailyCallCap: 10}); err != nil {
		t.Fatalf("set limits: %v", err)
	}
	limits, err := st.GetLimits(ctx, campaign.ID)
	if err != nil {
		t.Fatalf("get limits: %v", err)
	}
	if limits.DailyCallCap != 10 {
		t.Fatalf("daily cap = %d, want 10", limits.DailyCallCap)
	}
	if err := st.RecordHealthSnapshot(ctx, &model.CampaignHealthSnapshot{CampaignID: campaign.ID, ConnectRate: 0.5}); err != nil {
		t.Fatalf("record health: %v", err)
	}
	health, err := st.GetLatestHealth(ctx, campaign.ID)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.ConnectRate != 0.5 {
		t.Fatalf("connect rate = %v, want 0.5", health.ConnectRate)
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
