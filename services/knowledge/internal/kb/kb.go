package kb

import (
	"context"
	"errors"

	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
)

type Service struct {
	store store.Store
}

func New(s store.Store) *Service {
	return &Service{store: s}
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

// Publish atomically swaps the active pointer
func (svc *Service) Publish(ctx context.Context, versionID, actorID string) error {
	v, err := svc.store.GetKbVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if v.Status != model.VersionApproved {
		return errors.New("only approved versions can be published")
	}
	if err := svc.store.SetActiveVersion(ctx, v.ProjectID, versionID); err != nil {
		return err
	}
	if err := svc.store.UpdateKbVersionStatus(ctx, versionID, model.VersionPublished, "", ""); err != nil {
		return err
	}
	return svc.store.RecordApprovalEvent(ctx, &model.ApprovalEvent{
		VersionID: versionID,
		Action:    "published",
		ActorID:   actorID,
	})
}
