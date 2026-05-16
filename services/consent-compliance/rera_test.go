package consent_compliance_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/consent-compliance/internal/compliance"
	"github.com/lead/services/consent-compliance/internal/model"
	"github.com/lead/services/consent-compliance/internal/store"
)

// TestRERAGateMissingKB verifies that a real-estate campaign without any KB is blocked.
func TestRERAGateMissingKB(t *testing.T) {
	s := store.NewFake()
	result := compliance.CheckRERAGate(context.Background(), s, "campaign-no-kb", "real_estate")
	if result.Allowed {
		t.Error("expected campaign without approved KB to be blocked")
	}
	if result.Reason != "rera_gate" {
		t.Errorf("expected reason 'rera_gate', got %q", result.Reason)
	}
}

// TestRERAGateApprovedKB verifies that a campaign with a complete approved KB is allowed.
func TestRERAGateApprovedKB(t *testing.T) {
	s := store.NewFake()
	s.SetApprovedKB("campaign-with-kb", &model.KnowledgeBase{
		CampaignID:     "campaign-with-kb",
		Approved:       true,
		RERANumber:     "RERA-MH-12345",
		HasBrochure:    true,
		HasPriceSheet:  true,
		HasDisclaimers: true,
	})
	result := compliance.CheckRERAGate(context.Background(), s, "campaign-with-kb", "real_estate")
	if !result.Allowed {
		t.Errorf("expected approved KB campaign to be allowed, got reason %q", result.Reason)
	}
}

// TestRERAGatePartialKB verifies that a KB missing required fields still blocks.
func TestRERAGatePartialKB(t *testing.T) {
	s := store.NewFake()
	s.SetApprovedKB("campaign-partial-kb", &model.KnowledgeBase{
		CampaignID: "campaign-partial-kb",
		Approved:   true,
		RERANumber: "RERA-MH-99999",
		// Missing: HasBrochure, HasPriceSheet, HasDisclaimers
	})
	result := compliance.CheckRERAGate(context.Background(), s, "campaign-partial-kb", "real_estate")
	if result.Allowed {
		t.Error("expected partial KB campaign to be blocked")
	}
}

// TestRERAGateNonRealEstate verifies non-real-estate campaigns bypass the RERA gate.
func TestRERAGateNonRealEstate(t *testing.T) {
	s := store.NewFake()
	result := compliance.CheckRERAGate(context.Background(), s, "campaign-edu", "education")
	if !result.Allowed {
		t.Errorf("expected non-real-estate campaign to bypass RERA gate, got reason %q", result.Reason)
	}
}

// TestCheckOutreachBlockedBySuppression verifies suppression blocks outreach with correct reason.
func TestCheckOutreachBlockedBySuppression(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	s := store.NewFake()
	_ = s.AddSuppression(context.Background(), "+919876543210", "test-suppression")

	result := compliance.CheckOutreachAllowed(context.Background(), s, compliance.OutreachRequest{
		LeadID:    "lead-sup-1",
		Phone:     "+919876543210",
		Channel:   model.ChannelPhone,
		AtTime:    time.Date(2024, 1, 15, 10, 0, 0, 0, ist),
		WindowCfg: compliance.DefaultWindowConfig(),
	})
	if result.Allowed {
		t.Error("suppressed phone should be blocked")
	}
	if result.Reason != "suppression" {
		t.Errorf("expected reason 'suppression', got %q", result.Reason)
	}
}

// TestCheckOutreachBlockedByTRAI verifies real-estate phone calls without 140-series CLI are blocked.
func TestCheckOutreachBlockedByTRAI(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	s := store.NewFake()

	result := compliance.CheckOutreachAllowed(context.Background(), s, compliance.OutreachRequest{
		LeadID:       "lead-trai-1",
		Phone:        "+919876543211",
		Channel:      model.ChannelPhone,
		AtTime:       time.Date(2024, 1, 15, 10, 0, 0, 0, ist),
		CampaignType: "real_estate",
		CallerID:     "080-12345678", // not 140-series
		WindowCfg:    compliance.DefaultWindowConfig(),
	})
	if result.Allowed {
		t.Error("real-estate call without 140-series CLI should be blocked")
	}
	if result.Reason != "trai_140_series" {
		t.Errorf("expected reason 'trai_140_series', got %q", result.Reason)
	}
}
