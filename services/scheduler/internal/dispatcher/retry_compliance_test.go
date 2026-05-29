package dispatcher_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/dispatcher"
)

// ----- helpers --------------------------------------------------------------

// seedInFlight seeds one row with CampaignCtx-encoded retry limits, then
// marks it in_flight with providerCallID. Returns the row ID.
func seedInFlight(t *testing.T, q *dispatcher.FakeDialQueueStore, campaignID, leadID, providerCallID string, retryMax, retryBusyMin, retryNoAnswerMin int) string {
	t.Helper()
	ctx := context.Background()
	rowID := campaignID + ":" + leadID
	row := &dispatcher.DialRow{
		ID:            rowID,
		CampaignID:    campaignID,
		TenantID:      "tenant-1",
		LeadID:        leadID,
		Phone:         "+911234567890",
		ProjectID:     "proj-1",
		KbVersionID:   "kb-1",
		Status:        "pending",
		NextAttemptAt: time.Now().Add(-time.Second),
		Attempts:      0,
		CampaignCtx: map[string]any{
			"_retry_max":               float64(retryMax),
			"_retry_busy_min":          float64(retryBusyMin),
			"_retry_no_answer_min":     float64(retryNoAnswerMin),
			"_call_window_start_hour":  float64(10),
			"_call_window_end_hour":    float64(19),
			"_timezone":                "Asia/Kolkata",
		},
	}
	if err := q.SeedRows(ctx, []*dispatcher.DialRow{row}); err != nil {
		t.Fatalf("SeedRows: %v", err)
	}
	if err := q.MarkInFlight(ctx, rowID, providerCallID); err != nil {
		t.Fatalf("MarkInFlight: %v", err)
	}
	return rowID
}

// dispatcherWithFakeTelephony builds a Dispatcher wired to a fake telephony
// HTTP server. callCount is atomically incremented on each call POST.
func dispatcherWithFakeTelephony(q *dispatcher.FakeDialQueueStore, ss dispatcher.SuppressionStore, callCount *int32) (*dispatcher.Dispatcher, *httptest.Server) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/calls" && r.Method == http.MethodPost {
			atomic.AddInt32(callCount, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"provider_call_id": "fake-pcid",
				"status":           "queued",
			})
			return
		}
		http.Error(w, "not found", 404)
	}))

	sub := &fakeSubscriber{}
	rdb := newFakeRedis()
	d := dispatcher.New(sub, rdb, nil, q, dispatcher.Config{
		CampaignURL:          "http://unused",
		TelephonyURL:         srv.URL,
		LeadImportURL:        "http://unused",
		PublicWebhookBaseURL: "https://test.example.com",
	}, dispatcher.WithSuppressionStore(ss))
	return d, srv
}

// ----- TestOutcome_NoAnswer_RetryUntilExhausted -----------------------------

func TestOutcome_NoAnswer_RetryUntilExhausted(t *testing.T) {
	const (
		campaignID       = "camp-retry-1"
		leadID           = "lead-001"
		retryMax         = 3
		retryNoAnswerMin = 180
	)
	ctx := context.Background()
	q := dispatcher.NewFakeDialQueueStore()

	pcid := "pcid-001"
	seedInFlight(t, q, campaignID, leadID, pcid, retryMax, 20, retryNoAnswerMin)

	d := dispatcher.New(&fakeSubscriber{}, newFakeRedis(), nil, q, dispatcher.Config{
		CampaignURL: "http://unused", TelephonyURL: "http://unused", LeadImportURL: "http://unused",
	})

	// First no-answer: attempts=1, status=pending, next_attempt_at ~180m.
	if err := d.HandleOutcome(ctx, "evt-1", pcid, "no-answer"); err != nil {
		t.Fatalf("outcome 1: %v", err)
	}
	row := q.RowByLeadID(campaignID, leadID)
	if row.Status != "pending" {
		t.Errorf("after 1st no-answer: status=%s want pending", row.Status)
	}
	if row.Attempts != 1 {
		t.Errorf("after 1st no-answer: attempts=%d want 1", row.Attempts)
	}
	minExpected := time.Now().Add(170 * time.Minute)
	maxExpected := time.Now().Add(190 * time.Minute)
	if row.NextAttemptAt.Before(minExpected) || row.NextAttemptAt.After(maxExpected) {
		t.Errorf("next_attempt_at=%v not ~180m ahead (window [%v, %v])", row.NextAttemptAt, minExpected, maxExpected)
	}

	// 2nd attempt.
	if err := q.MarkInFlight(ctx, row.ID, "pcid-002"); err != nil {
		t.Fatalf("MarkInFlight 2: %v", err)
	}
	if err := d.HandleOutcome(ctx, "evt-2", "pcid-002", "no-answer"); err != nil {
		t.Fatalf("outcome 2: %v", err)
	}
	row = q.RowByLeadID(campaignID, leadID)
	if row.Status != "pending" || row.Attempts != 2 {
		t.Errorf("after 2nd: status=%s attempts=%d want pending/2", row.Status, row.Attempts)
	}

	// 3rd attempt → exhausted.
	if err := q.MarkInFlight(ctx, row.ID, "pcid-003"); err != nil {
		t.Fatalf("MarkInFlight 3: %v", err)
	}
	if err := d.HandleOutcome(ctx, "evt-3", "pcid-003", "no-answer"); err != nil {
		t.Fatalf("outcome 3: %v", err)
	}
	row = q.RowByLeadID(campaignID, leadID)
	if row.Status != "failed" {
		t.Errorf("exhausted: status=%s want failed", row.Status)
	}
	if row.Disposition != "exhausted" {
		t.Errorf("exhausted: disposition=%s want exhausted", row.Disposition)
	}
	if row.Attempts != 3 {
		t.Errorf("exhausted: attempts=%d want 3", row.Attempts)
	}
}

// ----- TestOutcome_Completed_NeverRetried -----------------------------------

func TestOutcome_Completed_NeverRetried(t *testing.T) {
	ctx := context.Background()
	q := dispatcher.NewFakeDialQueueStore()
	seedInFlight(t, q, "camp-done", "lead-001", "pcid-done", 3, 20, 180)

	d := dispatcher.New(&fakeSubscriber{}, newFakeRedis(), nil, q, dispatcher.Config{
		CampaignURL: "http://unused", TelephonyURL: "http://unused", LeadImportURL: "http://unused",
	})
	if err := d.HandleOutcome(ctx, "evt-done", "pcid-done", "completed"); err != nil {
		t.Fatalf("outcome: %v", err)
	}
	row := q.RowByLeadID("camp-done", "lead-001")
	if row.Status != "done" {
		t.Errorf("status=%s want done", row.Status)
	}
	if row.Disposition != "answered" {
		t.Errorf("disposition=%s want answered", row.Disposition)
	}
	// attempts must NOT change (was 0 before MarkInFlight increments it, which happens in pgkv store;
	// in the FakeStore MarkInFlight does NOT increment attempts — that's MarkInFlight's responsibility
	// in pgkv only). For completed we just check status/disposition.
}

// ----- TestOutcome_Idempotent -----------------------------------------------

func TestOutcome_Idempotent(t *testing.T) {
	ctx := context.Background()
	q := dispatcher.NewFakeDialQueueStore()
	seedInFlight(t, q, "camp-idemp", "lead-001", "pcid-idemp", 3, 20, 180)

	d := dispatcher.New(&fakeSubscriber{}, newFakeRedis(), nil, q, dispatcher.Config{
		CampaignURL: "http://unused", TelephonyURL: "http://unused", LeadImportURL: "http://unused",
	})

	// First delivery.
	if err := d.HandleOutcome(ctx, "evt-same", "pcid-idemp", "no-answer"); err != nil {
		t.Fatalf("first: %v", err)
	}
	row := q.RowByLeadID("camp-idemp", "lead-001")
	attemptsAfterFirst := row.Attempts

	// Redelivery of same event_id — pcid no longer in_flight (row is pending now),
	// but the idempotency guard checks _last_outcome_event_id stored in CampaignCtx.
	if err := d.HandleOutcome(ctx, "evt-same", "pcid-idemp", "no-answer"); err != nil {
		t.Fatalf("second: %v", err)
	}
	row = q.RowByLeadID("camp-idemp", "lead-001")
	if row.Attempts != attemptsAfterFirst {
		t.Errorf("idempotent redelivery incremented attempts: got %d want %d", row.Attempts, attemptsAfterFirst)
	}
}

// ----- TestCallingWindow_OutOfWindow ----------------------------------------

func TestCallingWindow_OutOfWindow(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	// 03:00 UTC = 08:30 IST — before window [10,19).
	now := time.Date(2026, 5, 29, 3, 0, 0, 0, time.UTC)

	ctx := context.Background()
	q := dispatcher.NewFakeDialQueueStore()
	ss := dispatcher.NewFakeSuppressionStore()

	rowID := "camp-win-1:lead-w1"
	row := &dispatcher.DialRow{
		ID:            rowID,
		CampaignID:    "camp-win-1",
		TenantID:      "tenant-w",
		LeadID:        "lead-w1",
		Phone:         "+911111111111",
		Status:        "pending",
		NextAttemptAt: now.Add(-time.Second),
		CampaignCtx: map[string]any{
			"_call_window_start_hour": float64(10),
			"_call_window_end_hour":   float64(19),
			"_timezone":               "Asia/Kolkata",
			"_retry_max":              float64(3),
		},
	}
	if err := q.SeedRows(ctx, []*dispatcher.DialRow{row}); err != nil {
		t.Fatalf("SeedRows: %v", err)
	}

	var callCount int32
	d, srv := dispatcherWithFakeTelephony(q, ss, &callCount)
	defer srv.Close()

	// Manually mark campaign active and run tick at out-of-window time.
	d.ActivateCampaign("camp-win-1")
	d.RunOneTickAt(ctx, now)

	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("call was placed outside calling window")
	}
	updated := q.RowByLeadID("camp-win-1", "lead-w1")
	if updated.Status != "pending" {
		t.Errorf("status=%s want pending", updated.Status)
	}
	// Expected: deferred to today 10:00 IST.
	expectedOpen := time.Date(2026, 5, 29, 10, 0, 0, 0, loc)
	if !updated.NextAttemptAt.Equal(expectedOpen) {
		t.Errorf("next_attempt_at=%v want %v", updated.NextAttemptAt, expectedOpen)
	}
}

// ----- TestCallingWindow_InsideWindow ----------------------------------------

func TestCallingWindow_InsideWindow(t *testing.T) {
	// 08:30 UTC = 14:00 IST — inside window.
	now := time.Date(2026, 5, 29, 8, 30, 0, 0, time.UTC)

	ctx := context.Background()
	q := dispatcher.NewFakeDialQueueStore()
	ss := dispatcher.NewFakeSuppressionStore()

	row := &dispatcher.DialRow{
		ID:            "camp-win-2:lead-w2",
		CampaignID:    "camp-win-2",
		TenantID:      "tenant-w2",
		LeadID:        "lead-w2",
		Phone:         "+912222222222",
		Status:        "pending",
		NextAttemptAt: now.Add(-time.Second),
		CampaignCtx: map[string]any{
			"_call_window_start_hour": float64(10),
			"_call_window_end_hour":   float64(19),
			"_timezone":               "Asia/Kolkata",
			"_retry_max":              float64(3),
		},
	}
	if err := q.SeedRows(ctx, []*dispatcher.DialRow{row}); err != nil {
		t.Fatalf("SeedRows: %v", err)
	}

	var callCount int32
	d, srv := dispatcherWithFakeTelephony(q, ss, &callCount)
	defer srv.Close()

	d.ActivateCampaign("camp-win-2")
	d.RunOneTickAt(ctx, now)

	if atomic.LoadInt32(&callCount) == 0 {
		t.Error("expected call to be placed inside calling window")
	}
}

// ----- TestSuppressed_NeverDialed -------------------------------------------

func TestSuppressed_NeverDialed(t *testing.T) {
	const (
		campaignID = "camp-sup-1"
		tenantID   = "tenant-sup"
		phone      = "+913333333333"
	)
	ctx := context.Background()
	q := dispatcher.NewFakeDialQueueStore()
	ss := dispatcher.NewFakeSuppressionStore()
	_ = ss.Suppress(ctx, tenantID, phone, "dnd")

	// Use a fixed "now" inside the calling window so only suppression is the gate.
	now := time.Date(2026, 5, 29, 8, 30, 0, 0, time.UTC) // 14:00 IST

	row := &dispatcher.DialRow{
		ID:            campaignID + ":lead-s1",
		CampaignID:    campaignID,
		TenantID:      tenantID,
		LeadID:        "lead-s1",
		Phone:         phone,
		Status:        "pending",
		NextAttemptAt: now.Add(-time.Second), // before the fixed "now"
		CampaignCtx: map[string]any{
			"_call_window_start_hour": float64(10),
			"_call_window_end_hour":   float64(19),
			"_timezone":               "Asia/Kolkata",
			"_retry_max":              float64(3),
		},
	}
	if err := q.SeedRows(ctx, []*dispatcher.DialRow{row}); err != nil {
		t.Fatalf("SeedRows: %v", err)
	}

	var callCount int32
	d, srv := dispatcherWithFakeTelephony(q, ss, &callCount)
	defer srv.Close()

	d.ActivateCampaign(campaignID)
	d.RunOneTickAt(ctx, now)

	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("suppressed phone should never be dialed")
	}
	updated := q.RowByLeadID(campaignID, "lead-s1")
	if updated.Status != "suppressed" {
		t.Errorf("status=%s want suppressed", updated.Status)
	}
	if updated.Disposition != "dnd" {
		t.Errorf("disposition=%s want dnd", updated.Disposition)
	}
}

// ----- TestNextWindowOpen ---------------------------------------------------

func TestNextWindowOpen(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	const startH, endH = 10, 19

	cases := []struct {
		name    string
		nowIST  time.Time
		wantIST time.Time
	}{
		{
			name:    "before open",
			nowIST:  time.Date(2026, 5, 29, 8, 30, 0, 0, loc),
			wantIST: time.Date(2026, 5, 29, 10, 0, 0, 0, loc),
		},
		{
			name:    "during window",
			nowIST:  time.Date(2026, 5, 29, 14, 0, 0, 0, loc),
			wantIST: time.Date(2026, 5, 30, 10, 0, 0, 0, loc),
		},
		{
			name:    "after close",
			nowIST:  time.Date(2026, 5, 29, 20, 0, 0, 0, loc),
			wantIST: time.Date(2026, 5, 30, 10, 0, 0, 0, loc),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dispatcher.NextWindowOpen(tc.nowIST, startH, endH, loc)
			if !got.Equal(tc.wantIST) {
				t.Errorf("got %v want %v", got, tc.wantIST)
			}
		})
	}
}
