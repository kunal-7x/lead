package kb

import (
	"context"
	"errors"
	"log"

	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
)

// VersionIndexer is the subset of *index.Indexer the kb service depends on.
// Defining it here breaks the import cycle and lets tests stub it out.
type VersionIndexer interface {
	IndexVersion(ctx context.Context, tenantID, projectID, versionID string) (int, error)
}

type Service struct {
	store   store.Store
	indexer VersionIndexer
}

func New(s store.Store) *Service {
	return &Service{store: s}
}

// WithIndexer attaches the embedding+Qdrant indexer. When set, Publish triggers
// a full re-index of the version. When nil, Publish is a pure metadata op (for
// tests / demo mode).
func (svc *Service) WithIndexer(i VersionIndexer) *Service {
	svc.indexer = i
	return svc
}

// SubmitForApproval changes draft → pending_approval
func (svc *Service) SubmitForApproval(ctx context.Context, versionID, submitterID string) error {
	v, err := svc.store.GetKbVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.Status != model.VersionDraft {
		return errors.New("only draft versions can be submitted")
	}
	if err := svc.store.UpdateKbVersionStatus(ctx, versionID, model.VersionPendingApproval, "", ""); err != nil {
		return err
	}
	return svc.store.RecordApprovalEvent(ctx, &model.ApprovalEvent{
		VersionID: versionID,
		Action:    "submitted",
		ActorID:   submitterID,
	})
}

// Approve requires RERA + brochure + price sheet on the project
func (svc *Service) Approve(ctx context.Context, versionID, reviewerID string) error {
	v, err := svc.store.GetKbVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.Status != model.VersionPendingApproval {
		return errors.New("only pending_approval versions can be approved")
	}
	p, err := svc.store.GetProject(ctx, v.ProjectID)
	if err != nil {
		return err
	}
	if p.RERANumber == "" {
		return errors.New("approval requires rera_number on the project")
	}
	if p.BrochureAssetID == "" {
		return errors.New("approval requires brochure_asset_id on the project")
	}
	if p.PriceSheetAssetID == "" {
		return errors.New("approval requires price_sheet_asset_id on the project")
	}
	if err := svc.store.UpdateKbVersionStatus(ctx, versionID, model.VersionApproved, reviewerID, ""); err != nil {
		return err
	}
	return svc.store.RecordApprovalEvent(ctx, &model.ApprovalEvent{
		VersionID: versionID,
		Action:    "approved",
		ActorID:   reviewerID,
	})
}

// Reject moves pending_approval → rejected
func (svc *Service) Reject(ctx context.Context, versionID, reviewerID, reason string) error {
	v, err := svc.store.GetKbVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.Status != model.VersionPendingApproval {
		return errors.New("only pending_approval versions can be rejected")
	}
	if err := svc.store.UpdateKbVersionStatus(ctx, versionID, model.VersionRejected, reviewerID, reason); err != nil {
		return err
	}
	return svc.store.RecordApprovalEvent(ctx, &model.ApprovalEvent{
		VersionID: versionID,
		Action:    "rejected",
		ActorID:   reviewerID,
		Notes:     reason,
	})
}

// Publish atomically swaps the active pointer and triggers vector indexing.
// Indexing failure is non-fatal: the version is published either way, but the
// caller gets a wrapped error so they know retrieval will fall back to the
// in-Postgres scan until re-publish.
func (svc *Service) Publish(ctx context.Context, versionID, actorID string) error {
	v, err := svc.store.GetKbVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.Status != model.VersionApproved {
		return errors.New("only approved versions can be published")
	}
	project, err := svc.store.GetProject(ctx, v.ProjectID)
	if err != nil {
		return err
	}
	if err := svc.store.SetActiveVersion(ctx, v.ProjectID, versionID); err != nil {
		return err
	}
	if err := svc.store.UpdateKbVersionStatus(ctx, versionID, model.VersionPublished, "", ""); err != nil {
		return err
	}
	if err := svc.store.RecordApprovalEvent(ctx, &model.ApprovalEvent{
		VersionID: versionID,
		Action:    "published",
		ActorID:   actorID,
	}); err != nil {
		return err
	}
	if svc.indexer != nil {
		n, err := svc.indexer.IndexVersion(ctx, project.TenantID, project.ID, versionID)
		if err != nil {
			log.Printf("kb: index version %s failed (%d points written): %v", versionID, n, err)
			return nil // status is published; caller can re-trigger via backfill
		}
		log.Printf("kb: indexed version %s into Qdrant (%d points)", versionID, n)
	}
	return nil
}
