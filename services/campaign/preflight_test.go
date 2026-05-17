package campaign_test

import (
	"context"
	"testing"

	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/preflight"
	"github.com/lead/services/campaign/internal/store"
)

func baseCampaign(s *store.Fake) *model.Campaign {
	s.SeedKbVersion(&model.KbVersion{
		ID:      "kb-approved",
		Status:  "approved",
		HasRERA: false,
	})
	s.SeedScript(&model.CampaignScript{CampaignID: "c1", Version: "v1", Content: "script"})
	s.SeedPrompt(&model.CampaignPromptVersion{CampaignID: "c1", Version: "v1", Prompt: "prompt"})
	return &model.Campaign{
		ID:              "c1",
		Name:            "test",
		ProjectID:       "p1",
		TenantID:        "t1",
		KbVersionID:     "kb-approved",
		ScriptVersionID: "v1",
		PromptVersionID: "v1",
		SourceFilter:    "phone",
		Schedule:        "09:00-21:00",
	}
}

func TestPreflight_Pass(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected pass, got errors: %v", res.Errors)
	}
}

func TestPreflight_KbNotApproved(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	s.SeedKbVersion(&model.KbVersion{ID: "kb-draft", Status: "draft"})
	c.KbVersionID = "kb-draft"
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for unapproved KB")
	}
	containsError(t, res.Errors, "not approved")
}

func TestPreflight_KbNotFound(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	c.KbVersionID = "nonexistent"
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing KB version")
	}
}

func TestPreflight_RealEstate_MissingRERA(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	s.SeedKbVersion(&model.KbVersion{
		ID:          "kb-re",
		Status:      "approved",
		ProjectType: model.ProjectTypeRealEstate,
		HasRERA:     false,
		HasBrochure: true,
		HasPriceSheet: true,
	})
	c.KbVersionID = "kb-re"
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing RERA")
	}
	containsError(t, res.Errors, "RERA")
}

func TestPreflight_RealEstate_MissingBrochure(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	s.SeedKbVersion(&model.KbVersion{
		ID:          "kb-re",
		Status:      "approved",
		ProjectType: model.ProjectTypeRealEstate,
		HasRERA:     true,
		HasBrochure: false,
		HasPriceSheet: true,
	})
	c.KbVersionID = "kb-re"
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing brochure")
	}
	containsError(t, res.Errors, "brochure")
}

func TestPreflight_RealEstate_MissingPriceSheet(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	s.SeedKbVersion(&model.KbVersion{
		ID:          "kb-re",
		Status:      "approved",
		ProjectType: model.ProjectTypeRealEstate,
		HasRERA:     true,
		HasBrochure: true,
		HasPriceSheet: false,
	})
	c.KbVersionID = "kb-re"
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing price sheet")
	}
	containsError(t, res.Errors, "price sheet")
}

func TestPreflight_MissingScriptVersion(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	c.ScriptVersionID = ""
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing script version")
	}
	containsError(t, res.Errors, "script_version_id")
}

func TestPreflight_ScriptVersionNotFound(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	c.ScriptVersionID = "nonexistent"
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for nonexistent script version")
	}
	containsError(t, res.Errors, "script_version")
}

func TestPreflight_MissingPromptVersion(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	c.PromptVersionID = ""
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing prompt version")
	}
	containsError(t, res.Errors, "prompt_version_id")
}

func TestPreflight_MissingSourceFilter(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	c.SourceFilter = ""
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing source filter")
	}
	containsError(t, res.Errors, "source_filter")
}

func TestPreflight_TenantSuspended(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	s.SeedTenant("t1", &model.TenantStatus{Suspended: true, BillingCapINR: 10000})
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for suspended tenant")
	}
	containsError(t, res.Errors, "suspended")
}

func TestPreflight_TenantBillingCapReached(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	s.SeedTenant("t1", &model.TenantStatus{Suspended: false, BillingCapINR: 1000, UsedINR: 1000})
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for billing cap reached")
	}
	containsError(t, res.Errors, "billing cap")
}

func TestPreflight_MissingSchedule(t *testing.T) {
	s := store.NewFake()
	c := baseCampaign(s)
	c.Schedule = ""
	checker := preflight.New(s)
	res, err := checker.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failure for missing schedule")
	}
	containsError(t, res.Errors, "schedule")
}

func containsError(t *testing.T, errs []string, substr string) {
	t.Helper()
	for _, e := range errs {
		if contains(e, substr) {
			return
		}
	}
	t.Fatalf("expected error containing %q, got: %v", substr, errs)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
