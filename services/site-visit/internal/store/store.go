package store

import (
	"context"

	"github.com/lead/services/site-visit/internal/model"
)

type Store interface {
	SaveVisit(context.Context, model.Visit) (model.Visit, error)
	GetVisit(context.Context, string) (model.Visit, error)
	ListVisits(context.Context, string) ([]model.Visit, error)
	AddEvent(context.Context, model.Event) (model.Event, error)
	ListEvents(context.Context, string) ([]model.Event, error)
	SaveWorkflowAction(context.Context, model.WorkflowAction) (model.WorkflowAction, error)
	ListWorkflowActions(context.Context, string) ([]model.WorkflowAction, error)
	MarkWorkflowActionFired(context.Context, string, string) (model.WorkflowAction, error)
}
