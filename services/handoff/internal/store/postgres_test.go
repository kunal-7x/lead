//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/handoff/internal/model"
)

func TestPostgresStoreHandoffRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	rep := model.Salesperson{TenantID: tenantID, TeamID: "team-a", Active: true}
	if err := st.SaveSalesperson(ctx, rep); err != nil {
		t.Fatalf("save rep: %v", err)
	}
	reps, err := st.ListSalespeople(ctx, tenantID, "team-a")
	if err != nil {
		t.Fatalf("list reps: %v", err)
	}
	if len(reps) != 1 {
		t.Fatalf("reps len = %d, want 1", len(reps))
	}
	task, err := st.SaveTask(ctx, model.Task{TenantID: tenantID, HandoffID: testID("handoff"), LeadID: testID("lead"), UserID: reps[0].ID, Status: model.TaskStatusOpen, DueAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	count, err := st.CountOpenTasks(ctx, task.UserID)
	if err != nil || count != 1 {
		t.Fatalf("open tasks count=%d err=%v", count, err)
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
