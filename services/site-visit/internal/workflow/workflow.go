package workflow

import (
	"time"

	"github.com/lead/services/site-visit/internal/model"
)

func ReminderActions(visit model.Visit) []model.WorkflowAction {
	slot := visit.ConfirmedSlot.Start
	return []model.WorkflowAction{
		action(visit, "pre_visit_whatsapp_24h", slot.Add(-24*time.Hour)),
		action(visit, "pre_visit_whatsapp_2h", slot.Add(-2*time.Hour)),
		action(visit, "pre_visit_voice_call_2h", slot.Add(-2*time.Hour)),
		action(visit, "pre_visit_whatsapp_15m", slot.Add(-15*time.Minute)),
		action(visit, "post_visit_whatsapp_summary_2h", slot.Add(2*time.Hour)),
		action(visit, "post_visit_voice_call_1d", slot.Add(24*time.Hour)),
		action(visit, "post_visit_followup_3d", slot.Add(72*time.Hour)),
		action(visit, "post_visit_followup_7d", slot.Add(168*time.Hour)),
	}
}

func NoShowRecoveryActions(visit model.Visit, now time.Time) []model.WorkflowAction {
	return []model.WorkflowAction{
		action(visit, "noshow_whatsapp_immediate", now),
		action(visit, "noshow_voice_attempt_1", now),
		action(visit, "noshow_voice_attempt_2", now.Add(2*time.Hour)),
		action(visit, "noshow_voice_attempt_3", now.Add(24*time.Hour)),
		action(visit, "noshow_loss_capture", now.Add(48*time.Hour)),
	}
}

func action(visit model.Visit, typ string, dueAt time.Time) model.WorkflowAction {
	return model.WorkflowAction{
		TenantID:  visit.TenantID,
		VisitID:   visit.ID,
		Type:      typ,
		DueAt:     dueAt.UTC(),
		CreatedAt: time.Now().UTC(),
	}
}
