package claims_test

import (
	"context"
	"testing"

	"github.com/lead/services/knowledge/internal/claims"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
)

func TestDefaultForbiddenPatternsBlock(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	project := createProject(t, ctx, st)
	svc := claims.New(st)

	cases := []string{
		"Guaranteed appreciation 20 percent yearly.",
		"Definite loan approval milega.",
		"Only today book now or lose this price.",
		"This ad has no RERA risk and cannot go wrong.",
		"Purchase decisions are not liable under RERA.",
	}
	for _, text := range cases {
		resp, err := svc.Check(ctx, claims.CheckRequest{
			TenantID:  project.TenantID,
			ProjectID: project.ID,
			Channel:   "voice",
			Text:      text,
		})
		if err != nil {
			t.Fatalf("check failed: %v", err)
		}
		if resp.OK {
			t.Fatalf("expected block for %q", text)
		}
	}
}

func TestApprovedClaimAllowsSensitiveText(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	project := createProject(t, ctx, st)
	svc := claims.New(st)

	allowed := &model.ProjectClaim{
		TenantID:    project.TenantID,
		ProjectID:   project.ID,
		ClaimType:   "price",
		Status:      model.ClaimStatusAllowed,
		Pattern:     "price starts at 1.5 crore",
		PatternKind: model.ClaimPatternPhrase,
	}
	if err := svc.SaveClaim(ctx, allowed); err != nil {
		t.Fatalf("save claim: %v", err)
	}
	resp, err := svc.Check(ctx, claims.CheckRequest{
		TenantID:  project.TenantID,
		ProjectID: project.ID,
		Channel:   "voice",
		Text:      "The price starts at 1.5 crore for approved units.",
	})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected approved claim to pass: %#v", resp)
	}
}

func TestGurugramAdRequiresHARERAWebsiteAndRERANumber(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	project := createProject(t, ctx, st)
	project.State = "Haryana"
	project.City = "Gurugram"
	project.RERANumber = "GGM/415/147/2020/31"
	if err := st.UpdateProject(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}
	svc := claims.New(st)

	resp, err := svc.Check(ctx, claims.CheckRequest{
		TenantID:  project.TenantID,
		ProjectID: project.ID,
		Channel:   "whatsapp",
		Text:      "Book a flat in Gurugram today.",
	})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if resp.OK {
		t.Fatal("expected Gurugram ad copy without HARERA website and RERA number to block")
	}
}

func createProject(t *testing.T, ctx context.Context, st *store.Fake) *model.Project {
	t.Helper()
	project := &model.Project{
		TenantID: "tenant-1",
		Name:     "Skyline Gurugram",
	}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return project
}
