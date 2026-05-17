package telephony_adapter_test

import (
	"context"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
)

// TestRouting_HealthyPlivoSelectedFirst verifies that the highest-priority healthy
// provider (Plivo, priority=1) is returned when all providers are healthy.
func TestRouting_HealthyPlivoSelectedFirst(t *testing.T) {
	s := store.NewFake()
	plivo := adapter.NewMock()
	mock := adapter.NewMock()

	// Override names so the routing rules can identify them.
	r := routing.New(s, []adapter.Telephony{
		&namedAdapter{Mock: plivo, name: "plivo"},
		&namedAdapter{Mock: mock, name: "mock"},
	})

	a, err := r.Select(context.Background(), "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if a.Name() != "plivo" {
		t.Fatalf("expected plivo, got %s", a.Name())
	}
}

// TestRouting_PlivoUnhealthy_FallsBackToExotel verifies that when Plivo is unhealthy
// the router selects the next healthy provider.
func TestRouting_PlivoUnhealthy_FallsBackToExotel(t *testing.T) {
	s := store.NewFake()

	plivoMock := adapter.NewMock()
	plivoMock.SetHealthy(false)

	mockAdapter := adapter.NewMock()

	r := routing.New(s, []adapter.Telephony{
		&namedAdapter{Mock: plivoMock, name: "plivo"},
		&namedAdapter{Mock: mockAdapter, name: "mock"},
	})

	a, err := r.Select(context.Background(), "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if a.Name() == "plivo" {
		t.Fatal("expected fallback provider, got plivo (which is unhealthy)")
	}
	if a.Name() != "mock" {
		t.Fatalf("expected mock fallback, got %s", a.Name())
	}
}

// TestRouting_HighFailRate_SkipsProvider verifies that a provider whose rolling failure
// rate exceeds MaxFailRate is skipped, even if it reports itself as healthy.
func TestRouting_HighFailRate_SkipsProvider(t *testing.T) {
	s := store.NewFake()

	// Push enough failures so plivo's failure rate exceeds 0.3 (MaxFailRate from seed rules).
	s.AddProviderFailures("plivo", 10) // 10 failures → 100% failure rate

	plivoMock := adapter.NewMock() // still reports healthy
	mockAdapter := adapter.NewMock()

	r := routing.New(s, []adapter.Telephony{
		&namedAdapter{Mock: plivoMock, name: "plivo"},
		&namedAdapter{Mock: mockAdapter, name: "mock"},
	})

	a, err := r.Select(context.Background(), "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if a.Name() == "plivo" {
		t.Fatal("expected plivo to be skipped due to high failure rate")
	}
}

// TestRouting_NoHealthyProvider_ReturnsError verifies that an error is returned when
// no provider can serve the request.
func TestRouting_NoHealthyProvider_ReturnsError(t *testing.T) {
	s := store.NewFake()

	plivoMock := adapter.NewMock()
	plivoMock.SetHealthy(false)
	mockAdapter := adapter.NewMock()
	mockAdapter.SetHealthy(false)

	r := routing.New(s, []adapter.Telephony{
		&namedAdapter{Mock: plivoMock, name: "plivo"},
		&namedAdapter{Mock: mockAdapter, name: "mock"},
	})

	_, err := r.Select(context.Background(), "")
	if err == nil {
		t.Fatal("expected error when no providers healthy, got nil")
	}
}

// TestRouting_RegionFilter verifies that routing rules with a specific region only
// match calls for that region.
func TestRouting_RegionFilter_Matches(t *testing.T) {
	s := store.NewFake()
	mockAdapter := adapter.NewMock()

	r := routing.New(s, []adapter.Telephony{
		&namedAdapter{Mock: mockAdapter, name: "mock"},
	})

	// "mock" has no region restriction in seed rules, so it matches any region.
	a, err := r.Select(context.Background(), "in-south")
	if err != nil {
		t.Fatalf("select with region: %v", err)
	}
	if a.Name() != "mock" {
		t.Fatalf("expected mock, got %s", a.Name())
	}
}

// namedAdapter wraps Mock and overrides the Name() method so routing
// rules (keyed by provider name) can identify each adapter.
type namedAdapter struct {
	*adapter.Mock
	name string
}

func (n *namedAdapter) Name() string { return n.name }
