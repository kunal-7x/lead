package store

import (
	"context"

	"github.com/lead/services/analytics-sink/internal/model"
)

type Store interface {
	InsertFact(context.Context, model.FactRow) (bool, error)
	CountFact(context.Context, string, string) (int, error)
	QueryReport(context.Context, string, string) (model.ReportResponse, error)
}
