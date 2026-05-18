package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lead/services/site-visit/internal/model"
	"github.com/lead/services/site-visit/internal/store"
	"github.com/lead/services/site-visit/internal/workflow"
)

var ErrInvalidTransition = errors.New("invalid site visit transition")

type Service struct {
	store store.Store
	now   func() time.Time
}

func New(st store.Store) *Service {
	return &Service{store: st, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) CreateTentative(ctx context.Context, req model.CreateTentativeRequest) (model.Visit, error) {
	if req.LeadID == "" || req.ProjectID == "" || len(req.ProposedSlots) == 0 {
		return model.Visit{}, errors.New("lead_id, project_id and proposed_slots are required")
	}
	if req.TenantID == "" {
		req.TenantID = "default"
	}
	now := s.now()
	visit, err := s.store.SaveVisit(ctx, model.Visit{
		TenantID:      req.TenantID,
		LeadID:        req.LeadID,
		ProjectID:     req.ProjectID,
		ProposedSlots: req.ProposedSlots,
		State:         model.StateTentative,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		return model.Visit{}, err
	}
	return s.event(ctx, visit, "", model.StateTentative, "visit_tentative_created", nil)
}

func (s *Service) Confirm(ctx context.Context, visitID string, slot model.ProposedSlot, salesRepID string) (model.Visit, error) {
	visit, err := s.store.GetVisit(ctx, visitID)
	if err != nil {
		return model.Visit{}, err
	}
	if visit.State != model.StateTentative && visit.State != model.StateRescheduled {
		return model.Visit{}, fmt.Errorf("%w: %s to confirmed", ErrInvalidTransition, visit.State)
	}
	from := visit.State
	visit.State = model.StateConfirmed
	visit.ConfirmedSlot = slot
	visit.SalesRepID = salesRepID
	visit, err = s.event(ctx, visit, from, model.StateConfirmed, "visit_confirmed", map[string]any{"sales_rep_id": salesRepID})
	if err != nil {
		return model.Visit{}, err
	}
	for _, action := range workflow.ReminderActions(visit) {
		if _, err := s.store.SaveWorkflowAction(ctx, action); err != nil {
			return model.Visit{}, err
		}
	}
	return visit, nil
}

func (s *Service) Reschedule(ctx context.Context, visitID string, newSlot model.ProposedSlot, reason string) (model.Visit, error) {
	visit, err := s.store.GetVisit(ctx, visitID)
	if err != nil {
		return model.Visit{}, err
	}
	if visit.State == model.StateCompleted || visit.State == model.StateLost {
		return model.Visit{}, fmt.Errorf("%w: %s to rescheduled", ErrInvalidTransition, visit.State)
	}
	from := visit.State
	visit.State = model.StateRescheduled
	visit.ConfirmedSlot = newSlot
	visit.ProposedSlots = []model.ProposedSlot{newSlot}
	return s.event(ctx, visit, from, model.StateRescheduled, "visit_rescheduled", map[string]any{"reason": reason})
}

func (s *Service) MarkCompleted(ctx context.Context, visitID, notes string, attendeeCount int) (model.Visit, error) {
	visit, err := s.store.GetVisit(ctx, visitID)
	if err != nil {
		return model.Visit{}, err
	}
	if visit.State != model.StateConfirmed && visit.State != model.StateReminderSent {
		return model.Visit{}, fmt.Errorf("%w: %s to completed", ErrInvalidTransition, visit.State)
	}
	from := visit.State
	visit.State = model.StateCompleted
	visit.Notes = notes
	visit.AttendeeCount = attendeeCount
	return s.event(ctx, visit, from, model.StateCompleted, "visit_completed", map[string]any{"attendee_count": attendeeCount})
}

func (s *Service) MarkNoShow(ctx context.Context, visitID string) (model.Visit, error) {
	visit, err := s.store.GetVisit(ctx, visitID)
	if err != nil {
		return model.Visit{}, err
	}
	if visit.State != model.StateConfirmed && visit.State != model.StateReminderSent {
		return model.Visit{}, fmt.Errorf("%w: %s to no_show", ErrInvalidTransition, visit.State)
	}
	from := visit.State
	visit.State = model.StateNoShow
	visit, err = s.event(ctx, visit, from, model.StateNoShow, "visit_no_show", nil)
	if err != nil {
		return model.Visit{}, err
	}
	for _, action := range workflow.NoShowRecoveryActions(visit, s.now()) {
		if _, err := s.store.SaveWorkflowAction(ctx, action); err != nil {
			return model.Visit{}, err
		}
	}
	return visit, nil
}

func (s *Service) MarkLost(ctx context.Context, visitID, reason string) (model.Visit, error) {
	visit, err := s.store.GetVisit(ctx, visitID)
	if err != nil {
		return model.Visit{}, err
	}
	if visit.State == model.StateCompleted {
		return model.Visit{}, fmt.Errorf("%w: completed to lost", ErrInvalidTransition)
	}
	from := visit.State
	visit.State = model.StateLost
	visit.LossReason = reason
	return s.event(ctx, visit, from, model.StateLost, "visit_lost", map[string]any{"reason": reason})
}

func (s *Service) EvaluateWorkflows(ctx context.Context, now time.Time) ([]model.WorkflowAction, error) {
	actions, err := s.store.ListWorkflowActions(ctx, "")
	if err != nil {
		return nil, err
	}
	slices.SortFunc(actions, func(a, b model.WorkflowAction) int {
		if cmp := a.DueAt.Compare(b.DueAt); cmp != 0 {
			return cmp
		}
		if a.Type < b.Type {
			return -1
		}
		if a.Type > b.Type {
			return 1
		}
		return 0
	})
	var fired []model.WorkflowAction
	for _, action := range actions {
		if !action.FiredAt.IsZero() || action.DueAt.After(now) {
			continue
		}
		visit, err := s.store.GetVisit(ctx, action.VisitID)
		if err != nil {
			return nil, err
		}
		if !canFirePreVisit(action.Type, visit.State) {
			continue
		}
		if isPostVisitFollowup(action.Type) && visit.State != model.StateCompleted {
			continue
		}
		if isNoShowRecovery(action.Type) && visit.State != model.StateNoShow {
			continue
		}
		firedAction, err := s.store.MarkWorkflowActionFired(ctx, action.ID, action.Type)
		if err != nil {
			return nil, err
		}
		firedAction.FiredAt = now.UTC()
		_, _ = s.store.AddEvent(ctx, model.Event{
			TenantID:  visit.TenantID,
			VisitID:   visit.ID,
			Type:      action.Type,
			FromState: visit.State,
			ToState:   visit.State,
			Payload:   map[string]any{"workflow_action_id": action.ID},
			CreatedAt: now.UTC(),
		})
		if action.Type == "pre_visit_whatsapp_24h" || action.Type == "pre_visit_whatsapp_2h" || action.Type == "pre_visit_whatsapp_15m" {
			visit.State = model.StateReminderSent
			visit, _ = s.store.SaveVisit(ctx, visit)
		}
		fired = append(fired, firedAction)
	}
	return fired, nil
}

func (s *Service) ListVisits(ctx context.Context, tenantID string) ([]model.Visit, error) {
	return s.store.ListVisits(ctx, tenantID)
}

func (s *Service) Store() store.Store {
	return s.store
}

func (s *Service) event(ctx context.Context, visit model.Visit, from, to model.State, typ string, payload map[string]any) (model.Visit, error) {
	visit, err := s.store.SaveVisit(ctx, visit)
	if err != nil {
		return model.Visit{}, err
	}
	_, err = s.store.AddEvent(ctx, model.Event{
		TenantID:  visit.TenantID,
		VisitID:   visit.ID,
		Type:      typ,
		FromState: from,
		ToState:   to,
		Payload:   payload,
		CreatedAt: s.now(),
	})
	return visit, err
}

func isPreVisitReminder(actionType string) bool {
	switch actionType {
	case "pre_visit_whatsapp_24h", "pre_visit_whatsapp_2h", "pre_visit_voice_call_2h", "pre_visit_whatsapp_15m":
		return true
	default:
		return false
	}
}

func canFirePreVisit(actionType string, state model.State) bool {
	if !isPreVisitReminder(actionType) {
		return true
	}
	if actionType == "pre_visit_whatsapp_24h" {
		return state == model.StateConfirmed
	}
	return state == model.StateConfirmed || state == model.StateReminderSent
}

func isPostVisitFollowup(actionType string) bool {
	switch actionType {
	case "post_visit_whatsapp_summary_2h", "post_visit_voice_call_1d", "post_visit_followup_3d", "post_visit_followup_7d":
		return true
	default:
		return false
	}
}

func isNoShowRecovery(actionType string) bool {
	switch actionType {
	case "noshow_whatsapp_immediate", "noshow_voice_attempt_1", "noshow_voice_attempt_2", "noshow_voice_attempt_3", "noshow_loss_capture":
		return true
	default:
		return false
	}
}
