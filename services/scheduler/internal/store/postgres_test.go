//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/model"
)

func TestPostgresStorePickAndMarkAttempt(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	campaignID := testID("campaign")
	leadID := testID("lead")
	if err := st.AddLeads(ctx, []*model.Lead{{ID: leadID, TenantID: tenantID, CampaignID: campaignID, PriorityScore: 9}}); err != nil {
		t.Fatalf("add leads: %v", err)
	}
	claimed, err := st.PickAndClaim(ctx, campaignID, "worker-1", 1, time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != leadID {
		t.Fatalf("claimed = %#v, want %s", claimed, leadID)
	}
	if err := st.SetCallSession(ctx, "call-"+leadID, leadID); err != nil {
		t.Fatalf("set call session: %v", err)
	}
	if err := st.MarkAttempt(ctx, "call-"+leadID, model.OutcomeNoAnswer); err != nil {
		t.Fatalf("mark attempt: %v", err)
	}
	depth, err := st.GetQueueDepth(ctx, tenantID)
	if err != nil {
		t.Fatalf("queue depth: %v", err)
	}
	if depth != 1 {
		t.Fatalf("depth = %d, want 1", depth)
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
