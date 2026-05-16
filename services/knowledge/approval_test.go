package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lead/services/knowledge/internal/kb"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
)

func setupApprovalTest(t *testing.T) (*store.Fake, *kb.Service, string, string) {
	t.Helper()
	ctx := context.Background()
	s := store.NewFake()
	svc := kb.New(s)

	// Create a project
	p := &model.Project{
		TenantID: "tenant-1",
		Name:     "Test Project",
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

	// Submit for approval
	if err := svc.SubmitForApproval(ctx, v.ID, "submitter-1"); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}

	return s, svc, p.ID, v.ID
}

func TestApproval_NoRERA(t *testing.T) {
	ctx := context.Background()
	s, svc, _, vID := setupApprovalTest(t)

	// Project has no RERA, brochure, or price_sheet
	_ = s
	err := svc.Approve(ctx, vID, "reviewer-1")
	if err == nil {
		t.Fatal("expected error for missing rera_number, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rera") {
		t.Fatalf("expected error to mention 'rera', got: %v", err)
	}
}

func TestApproval_NoBrochure(t *testing.T) {
	ctx := context.Background()
	s, svc, pID, vID := setupApprovalTest(t)

	// Update project with RERA but no brochure
	p, _ := s.GetProject(ctx, pID)
	p.RERANumber = "RERA-001"
	if err := s.UpdateProject(ctx, p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	err := svc.Approve(ctx, vID, "reviewer-1")
	if err == nil {
		t.Fatal("expected error for missing brochure_asset_id, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "brochure") {
		t.Fatalf("expected error to mention 'brochure', got: %v", err)
	}
}

func TestApproval_NoPriceSheet(t *testing.T) {
	ctx := context.Background()
	s, svc, pID, vID := setupApprovalTest(t)

	// Update project with RERA and brochure but no price_sheet
	p, _ := s.GetProject(ctx, pID)
	p.RERANumber = "RERA-001"
	p.BrochureAssetID = "brochure-asset-id"
	if err := s.UpdateProject(ctx, p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	err := svc.Approve(ctx, vID, "reviewer-1")
	if err == nil {
		t.Fatal("expected error for missing price_sheet_asset_id, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "price_sheet") {
		t.Fatalf("expected error to mention 'price_sheet', got: %v", err)
	}
}

func TestApproval_Success(t *testing.T) {
	ctx := context.Background()
	s, svc, pID, vID := setupApprovalTest(t)

	// Update project with all required fields
	p, _ := s.GetProject(ctx, pID)
	p.RERANumber = "RERA-001"
	p.BrochureAssetID = "brochure-asset-id"
	p.PriceSheetAssetID = "price-sheet-asset-id"
	if err := s.UpdateProject(ctx, p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	err := svc.Approve(ctx, vID, "reviewer-1")
	if err != nil {
		t.Fatalf("expected approval to succeed, got: %v", err)
	}

	// Verify status
	v, err := s.GetKbVersion(ctx, vID)
	if err != nil {
		t.Fatalf("GetKbVersion: %v", err)
	}
	if v.Status != model.VersionApproved {
		t.Fatalf("expected status %q, got %q", model.VersionApproved, v.Status)
	}
}
