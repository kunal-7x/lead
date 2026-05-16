//go:build temporal

package consent_compliance_test

import (
	"context"
	"testing"

	"github.com/lead/services/consent-compliance/internal/temporal"
)

// TestErasureWorkflow verifies the DPDP erasure workflow:
// - PII is redacted across all services
// - consent_ledger row is preserved (legal obligation)
// - workflow completes within 30 days SLA
func TestErasureWorkflow(t *testing.T) {
	result := temporal.RunErasureWorkflow(context.Background(), "lead-test-erasure-123")

	if !result.PIIRedacted {
		t.Error("expected PII to be redacted after erasure workflow")
	}
	if !result.LedgerRowPreserved {
		t.Error("expected consent ledger row to be preserved (legal requirement: DPDP)")
	}
	if result.DurationDays > 30 {
		t.Errorf("erasure SLA is 30 days, workflow took %d days", result.DurationDays)
	}
	if result.WorkflowID == "" {
		t.Error("expected non-empty workflow ID")
	}
	if result.LeadID != "lead-test-erasure-123" {
		t.Errorf("expected lead_id 'lead-test-erasure-123', got %q", result.LeadID)
	}
}
