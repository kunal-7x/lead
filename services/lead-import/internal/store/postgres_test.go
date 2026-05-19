//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/lead-import/internal/model"
)

func TestPostgresStoreLeadImportRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	id := testID("li")
	tenantID := testID("tenant")
	contact, err := st.UpsertContact(ctx, &model.Contact{TenantID: tenantID, PhoneE164: "+911234567890", Email: id + "@example.test"})
	if err != nil {
		t.Fatalf("upsert contact: %v", err)
	}
	lead := &model.Lead{TenantID: tenantID, ContactID: contact.ID, Status: "new", SourceID: testID("source")}
	if err := st.CreateLead(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}
	got, err := st.GetLead(ctx, tenantID, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if got.ContactID != contact.ID {
		t.Fatalf("contact id = %q, want %q", got.ContactID, contact.ID)
	}
	list, err := st.ListLeads(ctx, tenantID, LeadFilters{Status: "new"})
	if err != nil {
		t.Fatalf("list leads: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
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
