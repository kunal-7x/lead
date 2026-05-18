package store

import (
	"context"

	"github.com/lead/services/billing-meter/internal/model"
)

type Store interface {
	RecordUsageCost(context.Context, model.UsageEvent, model.CostEvent) (bool, error)
	GetSummary(context.Context, string, string) (model.Summary, error)
	SetCaps(context.Context, model.Caps) error
	GetCaps(context.Context, string, string) (model.Caps, error)
	AddCredit(context.Context, model.CreditLedgerEntry) (model.CreditLedgerEntry, error)
	GetBalance(context.Context, string) (float64, error)
	AddSignal(context.Context, model.Signal) (model.Signal, error)
	ListSignals(context.Context, string) ([]model.Signal, error)
	ListCostEvents(context.Context, string) ([]model.CostEvent, error)
}
