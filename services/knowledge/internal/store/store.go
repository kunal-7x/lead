package store

import (
	"context"

	"github.com/lead/services/knowledge/internal/model"
)

type Store interface {
	// Projects
	CreateProject(ctx context.Context, p *model.Project) error
	UpdateProject(ctx context.Context, p *model.Project) error
	GetProject(ctx context.Context, id string) (*model.Project, error)
	ListProjects(ctx context.Context, tenantID string) ([]*model.Project, error)

	// KB Versions
	CreateKbVersion(ctx context.Context, v *model.KbVersion) error
	GetKbVersion(ctx context.Context, id string) (*model.KbVersion, error)
	UpdateKbVersionStatus(ctx context.Context, id, status, reviewedBy, notes string) error
	SetActiveVersion(ctx context.Context, projectID, versionID string) error
	ListKbVersions(ctx context.Context, projectID string) ([]*model.KbVersion, error)

	// Content (all target a draft version)
	AddFact(ctx context.Context, f *model.Fact) error
	AddFAQ(ctx context.Context, f *model.FAQ) error
	AddAsset(ctx context.Context, a *model.Asset) error
	AddInventory(ctx context.Context, inv *model.Inventory) error
	AddOffer(ctx context.Context, o *model.Offer) error
	AddDisclaimer(ctx context.Context, d *model.Disclaimer) error

	// Listings (used by the embedding indexer at publish time)
	ListFactsByVersion(ctx context.Context, versionID string) ([]*model.Fact, error)
	ListFAQsByVersion(ctx context.Context, versionID string) ([]*model.FAQ, error)
	ListDisclaimersByVersion(ctx context.Context, versionID string) ([]*model.Disclaimer, error)

	// Approval history
	RecordApprovalEvent(ctx context.Context, e *model.ApprovalEvent) error

	// Retrieval
	RetrieveKb(ctx context.Context, projectID string, queryEmbedding []float32, topK int) (*model.RetrieveResult, error)

	// Pronunciation
	AddPronunciation(ctx context.Context, p *model.Pronunciation) error
	ListPronunciations(ctx context.Context, tenantID, lang string) ([]*model.Pronunciation, error)

	// Claim control
	SaveClaim(ctx context.Context, claim *model.ProjectClaim) error
	GetClaim(ctx context.Context, id string) (*model.ProjectClaim, error)
	ListClaims(ctx context.Context, projectID string) ([]*model.ProjectClaim, error)
	ListGlobalClaims(ctx context.Context) ([]*model.ProjectClaim, error)
	RecordClaimViolation(ctx context.Context, violation *model.ClaimViolation) error
	ListClaimViolations(ctx context.Context, projectID string) ([]*model.ClaimViolation, error)
}
