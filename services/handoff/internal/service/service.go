package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lead/services/handoff/internal/assignment"
	"github.com/lead/services/handoff/internal/model"
	"github.com/lead/services/handoff/internal/store"
)

var ErrSuppressionSuggested = errors.New("suppression suggested instead of human handoff")

type Service struct {
	store store.Store
	now   func() time.Time
}

func New(st store.Store) *Service {
	return &Service{
		store: st,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) CreateHandoff(ctx context.Context, req model.CreateHandoffRequest) (model.Handoff, error) {
	if req.LeadID == "" || req.ScoringSnapshotID == "" {
		return model.Handoff{}, errors.New("lead_id and scoring_snapshot_id are required")
	}
	snapshot, err := s.store.GetScoringSnapshot(ctx, req.ScoringSnapshotID)
	if err != nil {
		return model.Handoff{}, err
	}
	if req.TenantID == "" {
		req.TenantID = snapshot.TenantID
	}
	now := s.now()
	if snapshot.BuyerType == model.BuyerTypeBroker || snapshot.BuyerType == model.BuyerTypeFake {
		handoff, err := s.store.SaveHandoff(ctx, model.Handoff{
			TenantID:          req.TenantID,
			LeadID:            req.LeadID,
			Reason:            req.Reason,
			Summary:           req.Summary,
			ScoringSnapshotID: req.ScoringSnapshotID,
			Status:            model.HandoffStatusSuppressionSuggested,
			CreatedAt:         now,
		})
		if err != nil {
			return model.Handoff{}, err
		}
		_, _ = s.store.AddTaskEvent(ctx, model.TaskEvent{
			TenantID:  req.TenantID,
			HandoffID: handoff.ID,
			Type:      "suppression_suggested",
			Payload: map[string]any{
				"buyer_type": string(snapshot.BuyerType),
			},
			CreatedAt: now,
		})
		return handoff, nil
	}

	reps, err := s.store.ListSalespeople(ctx, req.TenantID, snapshot.TeamID)
	if err != nil {
		return model.Handoff{}, err
	}
	rep, err := assignment.Select(ctx, s.store, reps, snapshot)
	if err != nil {
		return model.Handoff{}, err
	}
	deadline := now.Add(slaDuration(req, snapshot))
	handoff, err := s.store.SaveHandoff(ctx, model.Handoff{
		TenantID:          req.TenantID,
		LeadID:            req.LeadID,
		Reason:            req.Reason,
		Summary:           req.Summary,
		ScoringSnapshotID: req.ScoringSnapshotID,
		AssignedUserID:    rep.ID,
		ManagerUserID:     rep.ManagerID,
		Status:            model.HandoffStatusPendingAck,
		SLADeadline:       deadline,
		CreatedAt:         now,
	})
	if err != nil {
		return model.Handoff{}, err
	}
	task, err := s.store.SaveTask(ctx, model.Task{
		TenantID:  req.TenantID,
		HandoffID: handoff.ID,
		LeadID:    req.LeadID,
		UserID:    rep.ID,
		Type:      "ack_handoff",
		Status:    model.TaskStatusOpen,
		DueAt:     deadline,
		CreatedAt: now,
	})
	if err != nil {
		return model.Handoff{}, err
	}
	if _, err := s.store.SaveLeadAssignment(ctx, model.LeadAssignment{
		TenantID:   req.TenantID,
		LeadID:     req.LeadID,
		UserID:     rep.ID,
		HandoffID:  handoff.ID,
		AssignedAt: now,
	}); err != nil {
		return model.Handoff{}, err
	}
	_, _ = s.store.AddTaskEvent(ctx, model.TaskEvent{
		TenantID:  req.TenantID,
		TaskID:    task.ID,
		HandoffID: handoff.ID,
		Type:      "handoff_created",
		Payload: map[string]any{
			"assigned_user_id": rep.ID,
			"temperature":      string(snapshot.Temperature),
		},
		CreatedAt: now,
	})
	_, _ = s.store.AddSLAEvent(ctx, model.SLAEvent{
		TenantID:  req.TenantID,
		HandoffID: handoff.ID,
		Stage:     "rep",
		Type:      "sla_started",
		UserID:    rep.ID,
		CreatedAt: now,
	})
	return handoff, nil
}

func (s *Service) AcknowledgeHandoff(ctx context.Context, handoffID, userID string) (model.Handoff, error) {
	handoff, err := s.store.GetHandoff(ctx, handoffID)
	if err != nil {
		return model.Handoff{}, err
	}
	if handoff.AssignedUserID != userID {
		return model.Handoff{}, fmt.Errorf("handoff assigned to %q, not %q", handoff.AssignedUserID, userID)
	}
	now := s.now()
	handoff.Status = model.HandoffStatusAcknowledged
	handoff.AcknowledgedAt = &now
	if err := s.store.CloseTasksForHandoff(ctx, handoff.ID, userID); err != nil {
		return model.Handoff{}, err
	}
	handoff, err = s.store.SaveHandoff(ctx, handoff)
	if err != nil {
		return model.Handoff{}, err
	}
	_, _ = s.store.AddTaskEvent(ctx, model.TaskEvent{
		TenantID:  handoff.TenantID,
		HandoffID: handoff.ID,
		Type:      "handoff_acknowledged",
		Payload:   map[string]any{"user_id": userID},
		CreatedAt: now,
	})
	return handoff, nil
}

func (s *Service) EscalateHandoff(ctx context.Context, handoffID string) (model.Handoff, error) {
	handoff, err := s.store.GetHandoff(ctx, handoffID)
	if err != nil {
		return model.Handoff{}, err
	}
	return s.escalate(ctx, handoff, "manual")
}

func (s *Service) ReassignHandoff(ctx context.Context, handoffID, newUserID string) (model.Handoff, error) {
	handoff, err := s.store.GetHandoff(ctx, handoffID)
	if err != nil {
		return model.Handoff{}, err
	}
	now := s.now()
	handoff.AssignedUserID = newUserID
	handoff.Status = model.HandoffStatusReassigned
	handoff.SLADeadline = now.Add(5 * time.Minute)
	handoff, err = s.store.SaveHandoff(ctx, handoff)
	if err != nil {
		return model.Handoff{}, err
	}
	if _, err := s.store.SaveLeadAssignment(ctx, model.LeadAssignment{
		TenantID:   handoff.TenantID,
		LeadID:     handoff.LeadID,
		UserID:     newUserID,
		HandoffID:  handoff.ID,
		AssignedAt: now,
	}); err != nil {
		return model.Handoff{}, err
	}
	_, _ = s.store.AddTaskEvent(ctx, model.TaskEvent{
		TenantID:  handoff.TenantID,
		HandoffID: handoff.ID,
		Type:      "handoff_reassigned",
		Payload:   map[string]any{"new_user_id": newUserID},
		CreatedAt: now,
	})
	return handoff, nil
}

func (s *Service) EvaluateSLAs(ctx context.Context, now time.Time) error {
	handoffs, err := s.store.ListHandoffs(ctx, "")
	if err != nil {
		return err
	}
	for _, handoff := range handoffs {
		if handoff.Status != model.HandoffStatusPendingAck && handoff.Status != model.HandoffStatusEscalated {
			continue
		}
		elapsed := now.Sub(handoff.CreatedAt)
		if elapsed >= 5*time.Minute && !s.store.HasSLAEvent(ctx, handoff.ID, "rep", "sla_missed") {
			_, _ = s.store.AddSLAEvent(ctx, model.SLAEvent{
				TenantID:  handoff.TenantID,
				HandoffID: handoff.ID,
				Stage:     "rep",
				Type:      "sla_missed",
				UserID:    handoff.AssignedUserID,
				CreatedAt: now,
			})
			if _, err := s.escalate(ctx, handoff, "sla_missed"); err != nil {
				return err
			}
		}
		if elapsed >= 10*time.Minute && !s.store.HasSLAEvent(ctx, handoff.ID, "manager", "sla_missed") {
			_, _ = s.store.AddSLAEvent(ctx, model.SLAEvent{
				TenantID:  handoff.TenantID,
				HandoffID: handoff.ID,
				Stage:     "manager",
				Type:      "sla_missed",
				UserID:    handoff.ManagerUserID,
				CreatedAt: now,
			})
		}
		if elapsed >= 20*time.Minute && !s.store.HasSLAEvent(ctx, handoff.ID, "tenant_owner", "alerted") {
			_, _ = s.store.AddSLAEvent(ctx, model.SLAEvent{
				TenantID:  handoff.TenantID,
				HandoffID: handoff.ID,
				Stage:     "tenant_owner",
				Type:      "alerted",
				CreatedAt: now,
			})
		}
	}
	return nil
}

func (s *Service) Store() store.Store {
	return s.store
}

func (s *Service) escalate(ctx context.Context, handoff model.Handoff, reason string) (model.Handoff, error) {
	now := s.now()
	handoff.Status = model.HandoffStatusEscalated
	handoff.EscalatedAt = &now
	handoff, err := s.store.SaveHandoff(ctx, handoff)
	if err != nil {
		return model.Handoff{}, err
	}
	_, _ = s.store.AddTaskEvent(ctx, model.TaskEvent{
		TenantID:  handoff.TenantID,
		HandoffID: handoff.ID,
		Type:      "handoff_escalated",
		Payload:   map[string]any{"reason": reason, "manager_user_id": handoff.ManagerUserID},
		CreatedAt: now,
	})
	_, _ = s.store.AddSLAEvent(ctx, model.SLAEvent{
		TenantID:  handoff.TenantID,
		HandoffID: handoff.ID,
		Stage:     "manager",
		Type:      "escalated",
		UserID:    handoff.ManagerUserID,
		CreatedAt: now,
	})
	return handoff, nil
}

func slaDuration(req model.CreateHandoffRequest, snapshot model.ScoringSnapshot) time.Duration {
	if snapshot.Temperature == model.TemperatureHot || snapshot.Temperature == model.TemperatureSuperHot {
		return 5 * time.Minute
	}
	if req.Reason == "site_visit_booking" || snapshot.ShouldCreateSiteVisit {
		return 30 * time.Minute
	}
	return 30 * time.Minute
}
