package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/whatsapp-adapter/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_whatsapp_adapter")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) SaveTemplate(ctx context.Context, tmpl model.Template) (model.Template, error) {
	now := time.Now().UTC()
	if tmpl.ID == "" {
		tmpl.ID = pgkv.NewID("")
	}
	if tmpl.CreatedAt.IsZero() {
		tmpl.CreatedAt = now
	}
	tmpl.UpdatedAt = now
	if err := p.kv.Put(ctx, "templates", tmpl.ID, tmpl); err != nil {
		return model.Template{}, err
	}
	return tmpl, p.kv.Put(ctx, "templates_by_name", templateNameKey(tmpl.TenantID, tmpl.Name, tmpl.Language), tmpl)
}

func (p *PostgresStore) GetTemplate(ctx context.Context, id string) (model.Template, error) {
	tmpl, ok, err := pgkv.Get[model.Template](ctx, p.kv, "templates", id)
	if err != nil {
		return model.Template{}, err
	}
	if !ok {
		return model.Template{}, fmt.Errorf("template %q: %w", id, ErrNotFound)
	}
	return tmpl, nil
}

func (p *PostgresStore) FindTemplateByName(ctx context.Context, tenantID, name, language string) (model.Template, error) {
	tmpl, ok, err := pgkv.Get[model.Template](ctx, p.kv, "templates_by_name", templateNameKey(tenantID, name, language))
	if err != nil {
		return model.Template{}, err
	}
	if !ok {
		return model.Template{}, fmt.Errorf("template %q/%q: %w", name, language, ErrNotFound)
	}
	return tmpl, nil
}

func (p *PostgresStore) ListTemplates(ctx context.Context, tenantID string) ([]model.Template, error) {
	templates, err := pgkv.List[model.Template](ctx, p.kv, "templates")
	if err != nil {
		return nil, err
	}
	out := make([]model.Template, 0, len(templates))
	for _, tmpl := range templates {
		if tenantID == "" || tmpl.TenantID == tenantID {
			out = append(out, tmpl)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (p *PostgresStore) SaveThread(ctx context.Context, thread model.Thread) (model.Thread, error) {
	now := time.Now().UTC()
	if thread.ID == "" {
		thread.ID = pgkv.NewID("")
	}
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = now
	}
	thread.UpdatedAt = now
	if err := p.kv.Put(ctx, "threads", thread.ID, thread); err != nil {
		return model.Thread{}, err
	}
	if thread.LeadID != "" {
		if err := p.kv.Put(ctx, "threads_by_lead", key(thread.TenantID, thread.LeadID), thread); err != nil {
			return model.Thread{}, err
		}
	}
	if thread.Phone != "" {
		if err := p.kv.Put(ctx, "threads_by_phone", key(thread.TenantID, thread.Phone), thread); err != nil {
			return model.Thread{}, err
		}
	}
	return thread, nil
}

func (p *PostgresStore) GetThread(ctx context.Context, id string) (model.Thread, error) {
	thread, ok, err := pgkv.Get[model.Thread](ctx, p.kv, "threads", id)
	if err != nil {
		return model.Thread{}, err
	}
	if !ok {
		return model.Thread{}, fmt.Errorf("thread %q: %w", id, ErrNotFound)
	}
	return thread, nil
}

func (p *PostgresStore) FindThreadByLead(ctx context.Context, tenantID, leadID string) (model.Thread, error) {
	thread, ok, err := pgkv.Get[model.Thread](ctx, p.kv, "threads_by_lead", key(tenantID, leadID))
	if err != nil {
		return model.Thread{}, err
	}
	if !ok {
		return model.Thread{}, fmt.Errorf("lead thread %q: %w", leadID, ErrNotFound)
	}
	return thread, nil
}

func (p *PostgresStore) FindThreadByPhone(ctx context.Context, tenantID, phone string) (model.Thread, error) {
	thread, ok, err := pgkv.Get[model.Thread](ctx, p.kv, "threads_by_phone", key(tenantID, phone))
	if err != nil {
		return model.Thread{}, err
	}
	if !ok {
		return model.Thread{}, fmt.Errorf("phone thread %q: %w", phone, ErrNotFound)
	}
	return thread, nil
}

func (p *PostgresStore) ListThreads(ctx context.Context, tenantID string) ([]model.Thread, error) {
	threads, err := pgkv.List[model.Thread](ctx, p.kv, "threads")
	if err != nil {
		return nil, err
	}
	out := make([]model.Thread, 0, len(threads))
	for _, thread := range threads {
		if tenantID == "" || thread.TenantID == tenantID {
			out = append(out, thread)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (p *PostgresStore) SaveMessage(ctx context.Context, msg model.Message) (model.Message, error) {
	if msg.ID == "" {
		msg.ID = pgkv.NewID("")
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	return msg, p.kv.Put(ctx, "messages", msg.ID, msg)
}

func (p *PostgresStore) ListMessages(ctx context.Context, threadID string) ([]model.Message, error) {
	messages, err := pgkv.List[model.Message](ctx, p.kv, "messages")
	if err != nil {
		return nil, err
	}
	out := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.ThreadID == threadID {
			out = append(out, msg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (p *PostgresStore) RecordWebhookEvent(ctx context.Context, event model.WebhookEvent) (bool, error) {
	if event.IdempotencyKey == "" {
		return false, errors.New("idempotency key is required")
	}
	if event.ID == "" {
		event.ID = pgkv.NewID("")
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	return p.kv.Insert(ctx, "webhook_events", event.IdempotencyKey, event)
}

func (p *PostgresStore) AddOptOut(ctx context.Context, opt model.WhatsAppOptOut) (model.WhatsAppOptOut, error) {
	if opt.ID == "" {
		opt.ID = pgkv.NewID("")
	}
	if opt.Channel == "" {
		opt.Channel = "whatsapp"
	}
	if opt.CreatedAt.IsZero() {
		opt.CreatedAt = time.Now().UTC()
	}
	return opt, p.kv.Put(ctx, "opt_outs", optOutKey(opt.TenantID, opt.LeadID, opt.Channel), opt)
}

func (p *PostgresStore) IsOptedOut(ctx context.Context, tenantID, leadID string) (bool, error) {
	_, ok, err := pgkv.Get[model.WhatsAppOptOut](ctx, p.kv, "opt_outs", optOutKey(tenantID, leadID, "whatsapp"))
	return ok, err
}

func (p *PostgresStore) ListOptOuts(ctx context.Context, tenantID string) ([]model.WhatsAppOptOut, error) {
	items, err := pgkv.List[model.WhatsAppOptOut](ctx, p.kv, "opt_outs")
	return filterTenant(items, tenantID), err
}

func (p *PostgresStore) AddConsentLedger(ctx context.Context, entry model.ConsentLedgerEntry) (model.ConsentLedgerEntry, error) {
	if entry.ID == "" {
		entry.ID = pgkv.NewID("")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	return entry, p.kv.Put(ctx, "consent_ledger", entry.ID, entry)
}

func (p *PostgresStore) ListConsentLedger(ctx context.Context, tenantID string) ([]model.ConsentLedgerEntry, error) {
	items, err := pgkv.List[model.ConsentLedgerEntry](ctx, p.kv, "consent_ledger")
	return filterTenant(items, tenantID), err
}

func (p *PostgresStore) AddOutboxEvent(ctx context.Context, event model.OutboxEvent) (model.OutboxEvent, error) {
	if event.ID == "" {
		event.ID = pgkv.NewID("")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, p.kv.Put(ctx, "outbox", event.ID, event)
}

func (p *PostgresStore) ListOutboxEvents(ctx context.Context, tenantID string) ([]model.OutboxEvent, error) {
	items, err := pgkv.List[model.OutboxEvent](ctx, p.kv, "outbox")
	return filterTenant(items, tenantID), err
}

func (p *PostgresStore) AddUsageEvent(ctx context.Context, event model.UsageEvent) (model.UsageEvent, error) {
	if event.ID == "" {
		event.ID = pgkv.NewID("")
	}
	if event.Quantity == 0 {
		event.Quantity = 1
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, p.kv.Put(ctx, "usage_events", event.ID, event)
}

func (p *PostgresStore) ListUsageEvents(ctx context.Context, tenantID string) ([]model.UsageEvent, error) {
	items, err := pgkv.List[model.UsageEvent](ctx, p.kv, "usage_events")
	return filterTenant(items, tenantID), err
}

func (p *PostgresStore) UpsertCredential(ctx context.Context, cred model.VaultCredential) (model.VaultCredential, error) {
	if cred.TenantID == "" {
		return model.VaultCredential{}, errors.New("tenant_id is required")
	}
	cred.UpdatedAt = time.Now().UTC()
	return cred, p.kv.Put(ctx, "credentials", cred.TenantID, cred)
}

func (p *PostgresStore) GetCredential(ctx context.Context, tenantID string) (model.VaultCredential, error) {
	cred, ok, err := pgkv.Get[model.VaultCredential](ctx, p.kv, "credentials", tenantID)
	if err != nil {
		return model.VaultCredential{}, err
	}
	if !ok {
		return model.VaultCredential{}, fmt.Errorf("credential %q: %w", tenantID, ErrNotFound)
	}
	return cred, nil
}

func (p *PostgresStore) EnqueueRetry(ctx context.Context, task model.RetryTask) (model.RetryTask, error) {
	if task.ID == "" {
		task.ID = pgkv.NewID("")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	return task, p.kv.Put(ctx, "retries", task.ID, task)
}

func (p *PostgresStore) ListRetryTasks(ctx context.Context, tenantID string) ([]model.RetryTask, error) {
	items, err := pgkv.List[model.RetryTask](ctx, p.kv, "retries")
	return filterTenant(items, tenantID), err
}

func templateNameKey(tenantID, name, language string) string {
	return tenantID + ":" + name + ":" + language
}

func optOutKey(tenantID, leadID, channel string) string {
	return tenantID + ":" + leadID + ":" + channel
}

type tenantScoped interface {
	model.WhatsAppOptOut | model.ConsentLedgerEntry | model.OutboxEvent | model.UsageEvent | model.RetryTask
}

func filterTenant[T tenantScoped](items []T, tenantID string) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		encoded := any(item)
		switch value := encoded.(type) {
		case model.WhatsAppOptOut:
			if tenantID == "" || value.TenantID == tenantID {
				out = append(out, item)
			}
		case model.ConsentLedgerEntry:
			if tenantID == "" || value.TenantID == tenantID {
				out = append(out, item)
			}
		case model.OutboxEvent:
			if tenantID == "" || value.TenantID == tenantID {
				out = append(out, item)
			}
		case model.UsageEvent:
			if tenantID == "" || value.TenantID == tenantID {
				out = append(out, item)
			}
		case model.RetryTask:
			if tenantID == "" || value.TenantID == tenantID {
				out = append(out, item)
			}
		}
	}
	return out
}
