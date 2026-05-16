package consent_compliance_test

import (
	"testing"
	"time"

	"github.com/lead/services/consent-compliance/internal/compliance"
)

// TestCallingWindowBlocked verifies 08:59 IST is outside the permitted window.
func TestCallingWindowBlocked(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	cfg := compliance.DefaultWindowConfig()

	t859 := time.Date(2024, 1, 15, 8, 59, 0, 0, ist)
	result := compliance.CheckCallingWindow(t859, cfg)
	if result.Allowed {
		t.Error("08:59 IST should be blocked by calling window")
	}
	if result.Reason != "calling_window" {
		t.Errorf("expected reason 'calling_window', got %q", result.Reason)
	}
}

// TestCallingWindowAllowed verifies 09:00 IST is within the permitted window.
func TestCallingWindowAllowed(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	cfg := compliance.DefaultWindowConfig()

	t900 := time.Date(2024, 1, 15, 9, 0, 0, 0, ist)
	result := compliance.CheckCallingWindow(t900, cfg)
	if !result.Allowed {
		t.Errorf("09:00 IST should be allowed, got reason %q", result.Reason)
	}
}

// TestCallingWindowEndExclusive verifies 21:00 IST is outside the window (end is exclusive).
func TestCallingWindowEndExclusive(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	cfg := compliance.DefaultWindowConfig()

	t2100 := time.Date(2024, 1, 15, 21, 0, 0, 0, ist)
	result := compliance.CheckCallingWindow(t2100, cfg)
	if result.Allowed {
		t.Error("21:00 IST should be blocked (end hour is exclusive)")
	}
}
