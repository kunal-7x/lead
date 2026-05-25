package telephony_adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/concurrency"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
)

// capFakeVobizHTTP is a local fake VobizHTTPClient for concurrency cap tests.
type capFakeVobizHTTP struct {
	uuid    string
	healthy bool
}

func (f *capFakeVobizHTTP) CreateCall(_ context.Context, _, _, _, _, _ string) (string, error) {
	return f.uuid, nil
}
func (f *capFakeVobizHTTP) HangupCall(_ context.Context, _, _, _ string) error { return nil }
func (f *capFakeVobizHTTP) TransferCall(_ context.Context, _, _, _, _ string) error { return nil }
func (f *capFakeVobizHTTP) GetRecordingURL(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (f *capFakeVobizHTTP) CheckHealth(_ context.Context, _ string) bool { return f.healthy }

// TestConcurrencyLimiter_FakeLimiter_CapEnforced confirms the FakeLimiter rejects
// calls beyond the per-tenant limit and rolls back the counter.
func TestConcurrencyLimiter_FakeLimiter_CapEnforced(t *testing.T) {
	const max = 3
	lim := concurrency.NewFake(max)
	ctx := context.Background()
	tenantID := "tenant-cap-test"

	// First 3 calls should be allowed.
	for i := 0; i < max; i++ {
		if err := lim.Incr(ctx, tenantID); err != nil {
			t.Fatalf("call %d of %d: expected nil error, got: %v", i+1, max, err)
		}
	}
	if got := lim.Count(tenantID); got != max {
		t.Fatalf("expected count=%d after %d allowed calls, got %d", max, max, got)
	}

	// 4th call should be rejected.
	if err := lim.Incr(ctx, tenantID); !errors.Is(err, concurrency.ErrConcurrencyLimit) {
		t.Fatalf("4th call: expected ErrConcurrencyLimit, got: %v", err)
	}
	// Counter should be rolled back.
	if got := lim.Count(tenantID); got != max {
		t.Fatalf("after limit rejection: expected count=%d (unchanged), got %d", max, got)
	}

	// 5th call also rejected.
	if err := lim.Incr(ctx, tenantID); !errors.Is(err, concurrency.ErrConcurrencyLimit) {
		t.Fatalf("5th call: expected ErrConcurrencyLimit, got: %v", err)
	}
}

// TestConcurrencyLimiter_Decr_FreesSlot confirms that Decr allows a previously-rejected call.
func TestConcurrencyLimiter_Decr_FreesSlot(t *testing.T) {
	const max = 2
	lim := concurrency.NewFake(max)
	ctx := context.Background()
	tenantID := "tenant-decr-test"

	// Fill to cap.
	for i := 0; i < max; i++ {
		if err := lim.Incr(ctx, tenantID); err != nil {
			t.Fatalf("fill call %d: %v", i+1, err)
		}
	}

	// Over cap.
	if err := lim.Incr(ctx, tenantID); !errors.Is(err, concurrency.ErrConcurrencyLimit) {
		t.Fatalf("over cap: expected ErrConcurrencyLimit")
	}

	// Decrement (simulate hangup).
	lim.Decr(ctx, tenantID)

	// Now one slot is free.
	if err := lim.Incr(ctx, tenantID); err != nil {
		t.Fatalf("after decr: expected nil error, got: %v", err)
	}
}

// TestConcurrencyLimiter_Noop_AlwaysAllows confirms the noop limiter never rejects.
func TestConcurrencyLimiter_Noop_AlwaysAllows(t *testing.T) {
	lim := concurrency.NewNoop()
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if err := lim.Incr(ctx, "any-tenant"); err != nil {
			t.Fatalf("noop limiter rejected call %d: %v", i, err)
		}
	}
}

// TestRouting_VobizConcurrencyCap_RejectedAtLimit wires a fake Vobiz adapter
// into the router and confirms PlaceCall returns ErrConcurrencyLimit for the
// 4th and 5th calls when the cap is 3.
func TestRouting_VobizConcurrencyCap_RejectedAtLimit(t *testing.T) {
	const max = 3

	s := store.NewFake()
	s.ResetRoutingRules()
	s.AddRoutingRule(model.ProviderRoutingRule{
		ID:          "rule-vobiz-cap",
		ProviderID:  "vobiz",
		Priority:    1,
		MaxFailRate: 1.0,
	})

	fakeVobizHTTP := &capFakeVobizHTTP{
		uuid:    "uuid-cap-test",
		healthy: true,
	}
	vobizAdapter := adapter.NewVobiz("aid", "tok", fakeVobizHTTP)

	lim := concurrency.NewFake(max)
	r := routing.NewWithLimiter(s, []adapter.Telephony{vobizAdapter}, lim)

	ctx := context.Background()
	req := model.CallRequest{
		TenantID:   "tenant-cap-routing",
		FromNumber: "+15550001111",
		ToNumber:   "+919999999999",
	}

	// First 3 calls succeed.
	for i := 0; i < max; i++ {
		req.SessionID = ""
		if _, err := r.PlaceCall(ctx, req); err != nil {
			t.Fatalf("call %d/%d: expected success, got: %v", i+1, max, err)
		}
	}

	// 4th and 5th calls are rejected with ErrConcurrencyLimit.
	for n, callNum := range []int{4, 5} {
		req.SessionID = ""
		_, err := r.PlaceCall(ctx, req)
		if err == nil {
			t.Fatalf("call %d: expected error, got nil", callNum)
		}
		if !errors.Is(err, concurrency.ErrConcurrencyLimit) {
			t.Fatalf("call %d (iteration %d): expected ErrConcurrencyLimit, got: %v", callNum, n, err)
		}
	}
}
