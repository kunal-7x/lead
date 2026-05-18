package store

import (
	"context"

	"github.com/lead/services/whatsapp-adapter/internal/model"
)

type Store interface {
	SaveTemplate(context.Context, model.Template) (model.Template, error)
	GetTemplate(context.Context, string) (model.Template, error)
	FindTemplateByName(context.Context, string, string, string) (model.Template, error)
	ListTemplates(context.Context, string) ([]model.Template, error)

	SaveThread(context.Context, model.Thread) (model.Thread, error)
	GetThread(context.Context, string) (model.Thread, error)
	FindThreadByLead(context.Context, string, string) (model.Thread, error)
	FindThreadByPhone(context.Context, string, string) (model.Thread, error)
	ListThreads(context.Context, string) ([]model.Thread, error)

	SaveMessage(context.Context, model.Message) (model.Message, error)
	ListMessages(context.Context, string) ([]model.Message, error)

	RecordWebhookEvent(context.Context, model.WebhookEvent) (bool, error)

	AddOptOut(context.Context, model.WhatsAppOptOut) (model.WhatsAppOptOut, error)
	IsOptedOut(context.Context, string, string) (bool, error)
	ListOptOuts(context.Context, string) ([]model.WhatsAppOptOut, error)

	AddConsentLedger(context.Context, model.ConsentLedgerEntry) (model.ConsentLedgerEntry, error)
	ListConsentLedger(context.Context, string) ([]model.ConsentLedgerEntry, error)

	AddOutboxEvent(context.Context, model.OutboxEvent) (model.OutboxEvent, error)
	ListOutboxEvents(context.Context, string) ([]model.OutboxEvent, error)

	AddUsageEvent(context.Context, model.UsageEvent) (model.UsageEvent, error)
	ListUsageEvents(context.Context, string) ([]model.UsageEvent, error)

	UpsertCredential(context.Context, model.VaultCredential) (model.VaultCredential, error)
	GetCredential(context.Context, string) (model.VaultCredential, error)

	EnqueueRetry(context.Context, model.RetryTask) (model.RetryTask, error)
	ListRetryTasks(context.Context, string) ([]model.RetryTask, error)
}
