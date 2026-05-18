package store

import (
	"context"

	"github.com/lead/services/handoff/internal/model"
)

type Store interface {
	SaveSalesperson(context.Context, model.Salesperson) error
	ListSalespeople(context.Context, string, string) ([]model.Salesperson, error)
	CountOpenTasks(context.Context, string) (int, error)

	SaveScoringSnapshot(context.Context, model.ScoringSnapshot) error
	GetScoringSnapshot(context.Context, string) (model.ScoringSnapshot, error)

	SaveHandoff(context.Context, model.Handoff) (model.Handoff, error)
	GetHandoff(context.Context, string) (model.Handoff, error)
	ListHandoffs(context.Context, string) ([]model.Handoff, error)

	SaveLeadAssignment(context.Context, model.LeadAssignment) (model.LeadAssignment, error)
	SaveTask(context.Context, model.Task) (model.Task, error)
	ListTasks(context.Context, string) ([]model.Task, error)
	CloseTasksForHandoff(context.Context, string, string) error

	AddTaskEvent(context.Context, model.TaskEvent) (model.TaskEvent, error)
	ListTaskEvents(context.Context, string) ([]model.TaskEvent, error)

	AddSLAEvent(context.Context, model.SLAEvent) (model.SLAEvent, error)
	ListSLAEvents(context.Context, string) ([]model.SLAEvent, error)
	HasSLAEvent(context.Context, string, string, string) bool
}
