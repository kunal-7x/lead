//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/consent-compliance/internal/model"
)

func TestPostgresStoreConsentAndSuppression(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	leadID := testID("lead")
	if err := st.RecordConsent(ctx, model.ConsentRecord{LeadID: leadID, Basis: model.ConsentBasisExplicit, Source: "test"}); err != nil {
		t.Fatalf("record consent: %v", err)
	}
	trail, err := st.GetConsentTrail(ctx, leadID)
	if err != nil {
		t.Fatalf("trail: %v", err)
	}
	if len(trail) != 1 {
		t.Fatalf("trail len = %d, want 1", len(trail))
	}
	phone := "+91" + strconv.FormatInt(time.Now().UnixNano()%10000000000, 10)
	if err := st.AddSuppression(ctx, phone, "test"); err != nil {
		t.Fatalf("add suppression: %v", err)
	}
	ok, err := st.IsSuppressed(ctx, phone)
	if err != nil || !ok {
		t.Fatalf("is suppressed: ok=%v err=%v", ok, err)
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
