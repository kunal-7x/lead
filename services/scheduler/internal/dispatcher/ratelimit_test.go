package dispatcher_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/dispatcher"
)

// newMockCampaignServerWithCaps returns a mock campaign server that
// includes rate-limit caps in its /limits response.
func newMockCampaignServerWithCaps(campaignID, tenantID, projectID string, leadIDs []string, hourlyCap, dailyCap int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/campaigns/"+campaignID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            campaignID,
				"tenant_id":     tenantID,
				"project_id":    projectID,
				"kb_version_id": "kb-v1",
				"context": map[string]any{
					"product_description": "Test",
					"language":            "en-IN",
					"goal":                "qualify",
				},
			})
		case r.URL.Path == "/v1/campaigns/"+campaignID+"/leads":
			type lead struct {
				LeadID string `json:"lead_id"`
			}
			leads := make([]lead, len(leadIDs))
			for i, id := range leadIDs {
				leads[i] = lead{LeadID: id}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"leads": leads})
		case r.URL.Path == "/v1/campaigns/"+campaignID+"/limits":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"max_call_seconds": 300,
				"concurrent_cap":   10,
				"hourly_call_cap":  hourlyCap,
				"daily_call_cap":   dailyCap,
				"cost_cap_inr":     0,
			})
		default:
			http.Error(w, "not found", 404)
		}
	}))
}

// TestRateLimit_HourlyCapBlocks verifies that the 3rd placement attempt within
// the same hour is blocked when hourlyCap=2, and the 1st and 2nd are allowed.
func TestRateLimit_HourlyCapBlocks(t *testing.T) {
	const campaignID = "camp-rl-001"
	rl := dispatcher.NewFakeRateLimiter()
	ctx := context.Background()

	// First call: allowed.
	ok, reason := rl.Allowed(ctx, campaignID, 2, 500, 0)
	if !ok {
		t.Fatalf("1st call should be allowed, got reason=%q", reason)
	}
	rl.RecordCall(ctx, campaignID)

	// Second call: allowed.
	ok, reason = rl.Allowed(ctx, campaignID, 2, 500, 0)
	if !ok {
		t.Fatalf("2nd call should be allowed, got reason=%q", reason)
	}
	rl.RecordCall(ctx, campaignID)

	// Third call: must be blocked.
	ok, reason = rl.Allowed(ctx, campaignID, 2, 500, 0)
	if ok {
		t.Fatalf("3rd call should be blocked (hourlyCap=2), got allowed=true")
	}
	if reason == "" {
		t.Error("expected non-empty reason when blocked")
	}
	t.Logf("3rd call blocked with reason: %s", reason)

	// Counter should be 2 (RecordCall was called twice).
	if got := rl.HourCount(campaignID); got != 2 {
		t.Errorf("expected hour count=2, got %d", got)
	}
}

// TestRateLimit_DefaultCapsApplied verifies that caps <= 0 fall back to defaults.
func TestRateLimit_DefaultCapsApplied(t *testing.T) {
	const campaignID = "camp-rl-defaults"
	rl := dispatcher.NewFakeRateLimiter()
	ctx := context.Background()

	// Set hour count to DefaultHourlyCap — next call must be blocked.
	rl.SetHourCount(campaignID, dispatcher.DefaultHourlyCap)

	ok, reason := rl.Allowed(ctx, campaignID, 0 /*use default*/, 500, 0)
	if ok {
		t.Fatalf("should be blocked at default hourly cap %d, got allowed=true", dispatcher.DefaultHourlyCap)
	}
	t.Logf("blocked at default cap: %s", reason)
}

// TestRateLimit_DailyCapBlocks verifies the daily cap is enforced independently.
func TestRateLimit_DailyCapBlocks(t *testing.T) {
	const campaignID = "camp-rl-daily"
	rl := dispatcher.NewFakeRateLimiter()
	ctx := context.Background()

	// Record up to dailyCap=3 calls.
	for i := 0; i < 3; i++ {
		ok, reason := rl.Allowed(ctx, campaignID, 50, 3, 0)
		if !ok {
			t.Fatalf("call %d should be allowed (dailyCap=3), got reason=%q", i+1, reason)
		}
		rl.RecordCall(ctx, campaignID)
	}

	// 4th call must be blocked by daily cap.
	ok, reason := rl.Allowed(ctx, campaignID, 50, 3, 0)
	if ok {
		t.Fatal("4th call should be blocked by daily cap=3")
	}
	t.Logf("daily cap block reason: %s", reason)
}

// TestRateLimit_CounterDoesNotIncrementOnBlock verifies that RecordCall is only
// called on success (simulated here by only calling RecordCall when Allowed).
func TestRateLimit_CounterDoesNotIncrementOnBlock(t *testing.T) {
	const campaignID = "camp-rl-noincr"
	rl := dispatcher.NewFakeRateLimiter()
	ctx := context.Background()

	// Fill to cap.
	rl.SetHourCount(campaignID, 2)

	// Blocked call — do NOT call RecordCall (as dispatcher does).
	ok, _ := rl.Allowed(ctx, campaignID, 2, 500, 0)
	if ok {
		t.Fatal("should be blocked")
	}
	// Counter must still be 2, not 3.
	if got := rl.HourCount(campaignID); got != 2 {
		t.Errorf("hour count should remain 2 after blocked call, got %d", got)
	}
}

// ----- Durable store round-trip (interface-level, in-memory) ----------------

// TestDialQueueStore_RoundTrip seeds rows, creates a fresh store backed by
// the same underlying map (simulating a restart), and asserts pending rows
// are returned.  For real Postgres, gate behind //go:build integration.
func TestDialQueueStore_RoundTrip(t *testing.T) {
	const (
		campaignID = "camp-store-001"
		tenantID   = "tenant-abc"
	)

	store := dispatcher.NewFakeDialQueueStore()
	ctx := context.Background()

	rows := []*dispatcher.DialRow{
		{
			ID:            campaignID + ":lead-001",
			CampaignID:    campaignID,
			TenantID:      tenantID,
			LeadID:        "lead-001",
			Phone:         "+919876500001",
			Status:        "pending",
			NextAttemptAt: time.Now().Add(-time.Second),
		},
		{
			ID:            campaignID + ":lead-002",
			CampaignID:    campaignID,
			TenantID:      tenantID,
			LeadID:        "lead-002",
			Phone:         "+919876500002",
			Status:        "pending",
			NextAttemptAt: time.Now().Add(-time.Second),
		},
	}

	// Seed rows.
	if err := store.SeedRows(ctx, rows); err != nil {
		t.Fatalf("SeedRows: %v", err)
	}

	// Verify PendingRows returns both.
	pending, err := store.PendingRows(ctx, campaignID, time.Now())
	if err != nil {
		t.Fatalf("PendingRows: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending rows, got %d", len(pending))
	}

	// SeedRows is idempotent — seeding the same rows again should not duplicate.
	if err := store.SeedRows(ctx, rows); err != nil {
		t.Fatalf("SeedRows (2nd): %v", err)
	}
	pending2, err := store.PendingRows(ctx, campaignID, time.Now())
	if err != nil {
		t.Fatalf("PendingRows after re-seed: %v", err)
	}
	if len(pending2) != 2 {
		t.Fatalf("idempotent seed: expected 2 rows, got %d", len(pending2))
	}

	// ActiveCampaignIDs should include campaignID.
	ids, err := store.ActiveCampaignIDs(ctx)
	if err != nil {
		t.Fatalf("ActiveCampaignIDs: %v", err)
	}
	found := false
	for _, id := range ids {
		if id == campaignID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ActiveCampaignIDs: expected %q, got %v", campaignID, ids)
	}

	// MarkInFlight one row; it should no longer appear in PendingRows.
	rowID := campaignID + ":lead-001"
	if err := store.MarkInFlight(ctx, rowID, "provider-123"); err != nil {
		t.Fatalf("MarkInFlight: %v", err)
	}
	pending3, err := store.PendingRows(ctx, campaignID, time.Now())
	if err != nil {
		t.Fatalf("PendingRows after MarkInFlight: %v", err)
	}
	if len(pending3) != 1 {
		t.Fatalf("expected 1 pending row after MarkInFlight, got %d", len(pending3))
	}
	if pending3[0].LeadID != "lead-002" {
		t.Errorf("expected remaining pending row to be lead-002, got %s", pending3[0].LeadID)
	}
}

// TestDispatcher_RateLimitEnforced integrates the FakeRateLimiter with the
// Dispatcher: with hourlyCap=2 served by the mock campaign server, the 3rd
// call placement attempt must be skipped (lead stays pending).
func TestDispatcher_RateLimitEnforced(t *testing.T) {
	const (
		campaignID = "camp-ratenforce-001"
		tenantID   = "tenant-rate"
		projectID  = "proj-rate"
	)

	leadIDs := []string{"lead-00001", "lead-00002", "lead-00003"}

	// Campaign server: returns hourlyCap=2.
	campSrv := newMockCampaignServerWithCaps(campaignID, tenantID, projectID, leadIDs, 2, 500)
	defer campSrv.Close()

	// Telephony: unlimited slots.
	tel := newMockTelephony(100)
	telSrv := httptest.NewServer(tel.handler())
	defer telSrv.Close()

	leadSrv := mockLeadImportServer(tenantID, leadIDs)
	defer leadSrv.Close()

	rdb := newFakeRedis()
	sub := &fakeSubscriber{}
	q := dispatcher.NewFakeDialQueueStore()
	rl := dispatcher.NewFakeRateLimiter()

	d := dispatcher.New(sub, rdb, nil, q, dispatcher.Config{
		CampaignURL:          campSrv.URL,
		LeadImportURL:        leadSrv.URL,
		TelephonyURL:         telSrv.URL,
		FromNumber:           "+911400000000",
		PublicWebhookBaseURL: "https://test.example.com",
	}, dispatcher.WithRateLimiter(rl))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	go d.Subscribe(ctx)
	time.Sleep(10 * time.Millisecond)
	sub.fire(campaignID)

	// Wait for all 3 rows to be seeded.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(q.AllRows()) < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(q.AllRows()) != 3 {
		t.Fatalf("expected 3 seeded rows, got %d", len(q.AllRows()))
	}

	// Tick 1: places leads 1 and 2, then rate-limited on lead 3.
	d.RunOneTick(ctx)

	// Wait briefly for the tick to complete.
	time.Sleep(20 * time.Millisecond)

	// Exactly 2 calls placed (hourlyCap=2).
	tel.mu.Lock()
	placed := int(tel.totalCalls)
	tel.mu.Unlock()
	if placed != 2 {
		t.Errorf("expected exactly 2 calls placed in tick (hourlyCap=2), got %d", placed)
	}

	// Hour counter in rate limiter must be 2.
	if got := rl.HourCount(campaignID); got != 2 {
		t.Errorf("expected rate limiter hour count=2, got %d", got)
	}

	// Exactly 1 lead must still be pending (the one that was not dialed due to rate-limit).
	// We do not assert on a specific lead ID because map iteration order is non-deterministic.
	allRows := q.AllRows()
	pendingCount := 0
	for _, r := range allRows {
		if r.Status == "pending" {
			pendingCount++
		}
	}
	if pendingCount != 1 {
		t.Errorf("expected exactly 1 pending row after rate-limit, got %d", pendingCount)
	}
}
