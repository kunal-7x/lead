// Package temporal provides a mock Temporal workflow for erasure.
// In production, replace RunErasureWorkflow with a real Temporal client call.
package temporal

import (
	"context"
	"time"
)

// ErasureResult describes the outcome of the DPDP erasure workflow.
type ErasureResult struct {
	WorkflowID         string
	LeadID             string
	PIIRedacted        bool
	LedgerRowPreserved bool // consent_ledger row is kept; only PII fields are blanked
	CompletedAt        time.Time
	DurationDays       int
}

// RunErasureWorkflow simulates the end-to-end erasure Temporal workflow.
// Steps: signal lead-import, lead-identity, call-sessions, WA threads to purge PII;
// redact consent-ledger PII fields while preserving the ledger row (legal obligation).
func RunErasureWorkflow(_ context.Context, leadID string) ErasureResult {
	start := time.Now()

	// All signals complete synchronously in this mock.
	// Real implementation: go.temporal.io/sdk/client.ExecuteWorkflow(...)

	elapsed := time.Since(start)
	durationDays := int(elapsed.Hours() / 24) // always 0 in tests

	return ErasureResult{
		WorkflowID:         "test-erasure-" + leadID,
		LeadID:             leadID,
		PIIRedacted:        true,
		LedgerRowPreserved: true,
		CompletedAt:        time.Now(),
		DurationDays:       durationDays,
	}
}
