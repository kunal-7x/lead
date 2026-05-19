//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/whatsapp-adapter/internal/model"
)

func TestPostgresStoreWhatsAppRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	tenantID := testID("tenant")
	tmpl, err := st.SaveTemplate(ctx, model.Template{TenantID: tenantID, Name: "welcome", Language: "en", Category: model.TemplateCategoryUtility, Body: "Hi", Status: "approved", MetaName: "welcome"})
	if err != nil {
		t.Fatalf("save template: %v", err)
	}
	found, err := st.FindTemplateByName(ctx, tenantID, "welcome", "en")
	if err != nil {
		t.Fatalf("find template: %v", err)
	}
	if found.ID != tmpl.ID {
		t.Fatalf("template id = %q, want %q", found.ID, tmpl.ID)
	}
	thread, err := st.SaveThread(ctx, model.Thread{TenantID: tenantID, LeadID: testID("lead"), Phone: "+911234"})
	if err != nil {
		t.Fatalf("save thread: %v", err)
	}
	if _, err := st.SaveMessage(ctx, model.Message{TenantID: tenantID, ThreadID: thread.ID, LeadID: thread.LeadID, Phone: thread.Phone, Direction: model.DirectionOutbound, Kind: model.MessageKindText, Body: "hello", Status: model.MessageStatusSent}); err != nil {
		t.Fatalf("save message: %v", err)
	}
	inserted, err := st.RecordWebhookEvent(ctx, model.WebhookEvent{TenantID: tenantID, IdempotencyKey: "hook-" + thread.ID, EventType: "message", Payload: []byte(`{}`)})
	if err != nil || !inserted {
		t.Fatalf("record webhook inserted=%v err=%v", inserted, err)
	}
	inserted, err = st.RecordWebhookEvent(ctx, model.WebhookEvent{TenantID: tenantID, IdempotencyKey: "hook-" + thread.ID, EventType: "message", Payload: []byte(`{}`)})
	if err != nil || inserted {
		t.Fatalf("duplicate webhook inserted=%v err=%v", inserted, err)
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
