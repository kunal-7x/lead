//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/site-visit/internal/model"
)

func TestPostgresStoreSiteVisitRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	visit, err := st.SaveVisit(ctx, model.Visit{TenantID: tenantID, LeadID: testID("lead"), ProjectID: testID("project"), State: model.StateTentative})
	if err != nil {
		t.Fatalf("save visit: %v", err)
	}
	got, err := st.GetVisit(ctx, visit.ID)
	if err != nil || got.ID != visit.ID {
		t.Fatalf("get visit id=%q err=%v", got.ID, err)
	}
	action, err := st.SaveWorkflowAction(ctx, model.WorkflowAction{TenantID: tenantID, VisitID: visit.ID, Type: "reminder", DueAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("save action: %v", err)
	}
	action, err = st.MarkWorkflowActionFired(ctx, action.ID, "tester")
	if err != nil {
		t.Fatalf("mark fired: %v", err)
	}
	if action.FiredAt.IsZero() {
		t.Fatal("fired_at not set")
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
