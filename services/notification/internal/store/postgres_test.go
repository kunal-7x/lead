//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/notification/internal/model"
)

func TestPostgresStoreNotificationRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	n, err := st.RecordNotification(ctx, model.Notification{TenantID: tenantID, Channel: model.ChannelEmail, Recipient: "a@example.test", Template: "welcome", Status: model.StatusSent})
	if err != nil {
		t.Fatalf("record notification: %v", err)
	}
	list, err := st.ListNotifications(ctx, tenantID)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(list) != 1 || list[0].ID != n.ID {
		t.Fatalf("notifications = %#v, want %q", list, n.ID)
	}
	if _, err := st.SaveSubscription(ctx, model.Subscription{TenantID: tenantID, EventTypes: []string{"lead.hot"}, Channels: []model.Channel{model.ChannelSlack}, Recipients: []string{"ops"}, Template: "hot"}); err != nil {
		t.Fatalf("save subscription: %v", err)
	}
	subs, err := st.ListSubscriptions(ctx, tenantID, "lead.hot")
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions len=%d err=%v", len(subs), err)
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
