package phone_test

import (
	"testing"

	"github.com/lead/services/lead-identity/internal/phone"
)

func TestNormalize(t *testing.T) {
	// All variants of the same Indian mobile number must resolve to +919876543210.
	target := "+919876543210"
	cases := []struct {
		raw     string
		country string
	}{
		{"+919876543210", "IN"},
		{"919876543210", "IN"},
		{"09876543210", "IN"},
		{"9876543210", "IN"},
		{"+91 98765 43210", "IN"},
		{"0091-9876543210", "IN"},
		{"+91-9876543210", "IN"},
		{"9876543210", "IN"},
		{"98765 43210", "IN"},
		{" +91 9876543210 ", "IN"},
	}
	for _, tc := range cases {
		got, err := phone.Normalize(tc.raw, tc.country)
		if err != nil {
			t.Errorf("Normalize(%q, %q) error: %v", tc.raw, tc.country, err)
			continue
		}
		if got != target {
			t.Errorf("Normalize(%q, %q) = %q; want %q", tc.raw, tc.country, got, target)
		}
	}
}

func TestNormalizeInvalid(t *testing.T) {
	_, err := phone.Normalize("notaphone", "IN")
	if err == nil {
		t.Error("expected error for invalid phone, got nil")
	}
}

func TestNormalizeEmpty(t *testing.T) {
	_, err := phone.Normalize("", "IN")
	if err == nil {
		t.Error("expected error for empty phone, got nil")
	}
}
