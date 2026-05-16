package consent_compliance_test

import (
	"context"
	"testing"

	"github.com/lead/services/consent-compliance/internal/store"
)

// TestAppendOnly verifies the consent_ledger is append-only:
// any attempt to UPDATE a ledger entry must return an error.
func TestAppendOnly(t *testing.T) {
	s := store.NewFake()
	err := s.UpdateConsent(context.Background(), "some-id")
	if err == nil {
		t.Fatal("expected error on UPDATE of consent_ledger (append-only), got nil")
	}
}
