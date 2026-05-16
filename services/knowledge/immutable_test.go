package knowledge_test

import (
	"context"
	"testing"

	"github.com/lead/services/knowledge/internal/kb"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
)

// buildAndPublishVersion creates a project, creates a draft version, adds a fact,
// goes through the full approval workflow and publishes it.
// Returns the fake store, the project ID, and the published version ID.
func buildAndPublishVersion(t *testing.T) (*store.Fake, *kb.Service, string, string) {
	t.Helper()
	ctx := context.Background()
	s := store.NewFake()
	svc := kb.New(s)

	// Create project with all required approval fields
	p := &model.Project{
		TenantID:          "tenant-1",
		Name:              "Immutable Test Project",
		RERANumber:        "RERA-100",
		BrochureAssetID:   "brochure-1",
		PriceSheetAssetID: "ps-1",
	}
	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Create a draft version
	v := &model.KbVersion{
		ProjectID: p.ID,
		Status:    model.VersionDraft,
	}
	if err := s.CreateKbVersion(ctx, v); err != nil {
		t.Fatalf("CreateKbVersion: %v", err)
	}

	// Add a fact to the draft
	fact := &model.Fact{
		VersionID: v.ID,
		Content:   "original fact",
	}
	if err := s.AddFact(ctx, fact); err != nil {
		t.Fatalf("AddFact: %v", err)
	}

	// Submit → Approve → Publish
	if err := svc.SubmitForApproval(ctx, v.ID, "actor-1"); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	if err := svc.Approve(ctx, v.ID, "reviewer-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := svc.Publish(ctx, v.ID, "actor-1"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	return s, svc, p.ID, v.ID
}

func TestImmutable_AddToPublishedVersion(t *testing.T) {
	ctx := context.Background()
	s, _, _, publishedVersionID := buildAndPublishVersion(t)

	// Try to add a fact to the published (non-draft) version — must fail
	err := s.AddFact(ctx, &model.Fact{
		VersionID: publishedVersionID,
		Content:   "should fail",
	})
	if err == nil {
		t.Fatal("expected error when adding fact to published version, got nil")
	}
}

func TestImmutable_NewDraftAfterPublish(t *testing.T) {
	ctx := context.Background()
	s, _, projectID, _ := buildAndPublishVersion(t)

	// Create a new draft version — must succeed
	v2 := &model.KbVersion{
		ProjectID: projectID,
		Status:    model.VersionDraft,
	}
	if err := s.CreateKbVersion(ctx, v2); err != nil {
		t.Fatalf("expected new draft to succeed, got: %v", err)
	}

	if v2.VersionNumber != 2 {
		t.Fatalf("expected version_number 2, got %d", v2.VersionNumber)
	}
}

func TestImmutable_AddFactToNewDraft(t *testing.T) {
	ctx := context.Background()
	s, _, projectID, _ := buildAndPublishVersion(t)

	// Create a new draft version
	v2 := &model.KbVersion{
		ProjectID: projectID,
		Status:    model.VersionDraft,
	}
	if err := s.CreateKbVersion(ctx, v2); err != nil {
		t.Fatalf("CreateKbVersion v2: %v", err)
	}

	// Add a fact to the new draft — must succeed
	err := s.AddFact(ctx, &model.Fact{
		VersionID: v2.ID,
		Content:   "fact in new draft",
	})
	if err != nil {
		t.Fatalf("expected AddFact to new draft to succeed, got: %v", err)
	}
}
