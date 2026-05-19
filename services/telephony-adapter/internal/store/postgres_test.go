//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/telephony-adapter/internal/model"
)

func TestPostgresStoreTelephonyRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	providers, err := st.ListProviders(ctx)
	if err != nil {
		t.Fatalf("providers: %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("providers empty")
	}
	sessionID := testID("session")
	if err := st.StoreCallSession(ctx, &model.CallSession{ID: sessionID, TenantID: testID("tenant"), FromNumber: "+911", ToNumber: "+922"}); err != nil {
		t.Fatalf("store session: %v", err)
	}
	got, err := st.GetCallSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != sessionID {
		t.Fatalf("session id = %q, want %q", got.ID, sessionID)
	}
	if err := st.SetIdempotencyKey(ctx, "idem-"+sessionID, []byte("ok")); err != nil {
		t.Fatalf("set idempotency: %v", err)
	}
	_, ok, err := st.CheckIdempotencyKey(ctx, "idem-"+sessionID)
	if err != nil || !ok {
		t.Fatalf("check idempotency ok=%v err=%v", ok, err)
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
