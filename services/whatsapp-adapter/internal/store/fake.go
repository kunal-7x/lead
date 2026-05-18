package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/whatsapp-adapter/internal/model"
)

var ErrNotFound = errors.New("not found")

type Fake struct {
	mu sync.Mutex

	templates        map[string]*model.Template
	threads          map[string]*model.Thread
	threadByLead     map[string]string
	threadByPhone    map[string]string
	messages         map[string]*model.Message
	messagesByThread map[string][]string
	webhookEvents    map[string]*model.WebhookEvent
	optOuts          map[string]*model.WhatsAppOptOut
	outbox           []*model.OutboxEvent
	usage            []*model.UsageEvent
	ledger           []*model.ConsentLedgerEntry
	credentials      map[string]*model.VaultCredential
	retries          []*model.RetryTask
}

func NewFake() *Fake {
	return &Fake{
		templates:        make(map[string]*model.Template),
		threads:          make(map[string]*model.Thread),
		threadByLead:     make(map[string]string),
		threadByPhone:    make(map[string]string),
		messages:         make(map[string]*model.Message),
		messagesByThread: make(map[string][]string),
		webhookEvents:    make(map[string]*model.WebhookEvent),
		optOuts:          make(map[string]*model.WhatsAppOptOut),
		credentials:      make(map[string]*model.VaultCredential),
	}
}

func (f *Fake) SaveTemplate(_ context.Context, tmpl model.Template) (model.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	if tmpl.ID == "" {
		tmpl.ID = uuid.NewString()
	}
	if tmpl.CreatedAt.IsZero() {
		tmpl.CreatedAt = now
	}
	tmpl.UpdatedAt = now
	cp := cloneTemplate(tmpl)
	f.templates[tmpl.ID] = &cp
	return tmpl, nil
}

func (f *Fake) GetTemplate(_ context.Context, id string) (model.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tmpl, ok := f.templates[id]
	if !ok {
		return model.Template{}, fmt.Errorf("template %q: %w", id, ErrNotFound)
	}
	return cloneTemplate(*tmpl), nil
}

func (f *Fake) FindTemplateByName(_ context.Context, tenantID, name, language string) (model.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, tmpl := range f.templates {
		if tmpl.TenantID == tenantID && tmpl.Name == name && tmpl.Language == language {
			return cloneTemplate(*tmpl), nil
		}
	}
	return model.Template{}, fmt.Errorf("template %q/%q: %w", name, language, ErrNotFound)
}

func (f *Fake) ListTemplates(_ context.Context, tenantID string) ([]model.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Template, 0, len(f.templates))
	for _, tmpl := range f.templates {
		if tenantID == "" || tmpl.TenantID == tenantID {
			out = append(out, cloneTemplate(*tmpl))
		}
	}
	slices.SortFunc(out, func(a, b model.Template) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out, nil
}

func (f *Fake) SaveThread(_ context.Context, thread model.Thread) (model.Thread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	if thread.ID == "" {
		thread.ID = uuid.NewString()
	}
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = now
	}
	thread.UpdatedAt = now
	cp := thread
	f.threads[thread.ID] = &cp
	if thread.LeadID != "" {
		f.threadByLead[key(thread.TenantID, thread.LeadID)] = thread.ID
	}
	if thread.Phone != "" {
		f.threadByPhone[key(thread.TenantID, thread.Phone)] = thread.ID
	}
	return thread, nil
}

func (f *Fake) GetThread(_ context.Context, id string) (model.Thread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	thread, ok := f.threads[id]
	if !ok {
		return model.Thread{}, fmt.Errorf("thread %q: %w", id, ErrNotFound)
	}
	return *thread, nil
}

func (f *Fake) FindThreadByLead(_ context.Context, tenantID, leadID string) (model.Thread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.threadByLead[key(tenantID, leadID)]
	if !ok {
		return model.Thread{}, fmt.Errorf("lead thread %q: %w", leadID, ErrNotFound)
	}
	return *f.threads[id], nil
}

func (f *Fake) FindThreadByPhone(_ context.Context, tenantID, phone string) (model.Thread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.threadByPhone[key(tenantID, phone)]
	if !ok {
		return model.Thread{}, fmt.Errorf("phone thread %q: %w", phone, ErrNotFound)
	}
	return *f.threads[id], nil
}

func (f *Fake) ListThreads(_ context.Context, tenantID string) ([]model.Thread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Thread, 0, len(f.threads))
	for _, thread := range f.threads {
		if tenantID == "" || thread.TenantID == tenantID {
			out = append(out, *thread)
		}
	}
	slices.SortFunc(out, func(a, b model.Thread) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	return out, nil
}

func (f *Fake) SaveMessage(_ context.Context, msg model.Message) (model.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	cp := cloneMessage(msg)
	if _, exists := f.messages[msg.ID]; !exists {
		f.messagesByThread[msg.ThreadID] = append(f.messagesByThread[msg.ThreadID], msg.ID)
	}
	f.messages[msg.ID] = &cp
	return msg, nil
}

func (f *Fake) ListMessages(_ context.Context, threadID string) ([]model.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := f.messagesByThread[threadID]
	out := make([]model.Message, 0, len(ids))
	for _, id := range ids {
		if msg, ok := f.messages[id]; ok {
			out = append(out, cloneMessage(*msg))
		}
	}
	return out, nil
}

func (f *Fake) RecordWebhookEvent(_ context.Context, event model.WebhookEvent) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.IdempotencyKey == "" {
		return false, errors.New("idempotency key is required")
	}
	if _, exists := f.webhookEvents[event.IdempotencyKey]; exists {
		return false, nil
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	cp := event
	cp.Payload = append([]byte(nil), event.Payload...)
	f.webhookEvents[event.IdempotencyKey] = &cp
	return true, nil
}

func (f *Fake) AddOptOut(_ context.Context, opt model.WhatsAppOptOut) (model.WhatsAppOptOut, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if opt.ID == "" {
		opt.ID = uuid.NewString()
	}
	if opt.Channel == "" {
		opt.Channel = "whatsapp"
	}
	if opt.CreatedAt.IsZero() {
		opt.CreatedAt = time.Now().UTC()
	}
	cp := opt
	f.optOuts[key(opt.TenantID, opt.LeadID)] = &cp
	return opt, nil
}

func (f *Fake) IsOptedOut(_ context.Context, tenantID, leadID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.optOuts[key(tenantID, leadID)]
	return ok, nil
}

func (f *Fake) ListOptOuts(_ context.Context, tenantID string) ([]model.WhatsAppOptOut, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.WhatsAppOptOut, 0, len(f.optOuts))
	for _, opt := range f.optOuts {
		if tenantID == "" || opt.TenantID == tenantID {
			out = append(out, *opt)
		}
	}
	return out, nil
}

func (f *Fake) AddConsentLedger(_ context.Context, entry model.ConsentLedgerEntry) (model.ConsentLedgerEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	cp := entry
	f.ledger = append(f.ledger, &cp)
	return entry, nil
}

func (f *Fake) ListConsentLedger(_ context.Context, tenantID string) ([]model.ConsentLedgerEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.ConsentLedgerEntry, 0, len(f.ledger))
	for _, entry := range f.ledger {
		if tenantID == "" || entry.TenantID == tenantID {
			out = append(out, *entry)
		}
	}
	return out, nil
}

func (f *Fake) AddOutboxEvent(_ context.Context, event model.OutboxEvent) (model.OutboxEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	cp := cloneOutbox(event)
	f.outbox = append(f.outbox, &cp)
	return event, nil
}

func (f *Fake) ListOutboxEvents(_ context.Context, tenantID string) ([]model.OutboxEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.OutboxEvent, 0, len(f.outbox))
	for _, event := range f.outbox {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, cloneOutbox(*event))
		}
	}
	return out, nil
}

func (f *Fake) AddUsageEvent(_ context.Context, event model.UsageEvent) (model.UsageEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Quantity == 0 {
		event.Quantity = 1
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	cp := event
	f.usage = append(f.usage, &cp)
	return event, nil
}

func (f *Fake) ListUsageEvents(_ context.Context, tenantID string) ([]model.UsageEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.UsageEvent, 0, len(f.usage))
	for _, event := range f.usage {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, *event)
		}
	}
	return out, nil
}

func (f *Fake) UpsertCredential(_ context.Context, cred model.VaultCredential) (model.VaultCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cred.TenantID == "" {
		return model.VaultCredential{}, errors.New("tenant_id is required")
	}
	cred.UpdatedAt = time.Now().UTC()
	cp := cred
	f.credentials[cred.TenantID] = &cp
	return cred, nil
}

func (f *Fake) GetCredential(_ context.Context, tenantID string) (model.VaultCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cred, ok := f.credentials[tenantID]
	if !ok {
		return model.VaultCredential{}, fmt.Errorf("credential %q: %w", tenantID, ErrNotFound)
	}
	return *cred, nil
}

func (f *Fake) EnqueueRetry(_ context.Context, task model.RetryTask) (model.RetryTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	cp := task
	f.retries = append(f.retries, &cp)
	return task, nil
}

func (f *Fake) ListRetryTasks(_ context.Context, tenantID string) ([]model.RetryTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.RetryTask, 0, len(f.retries))
	for _, task := range f.retries {
		if tenantID == "" || task.TenantID == tenantID {
			out = append(out, *task)
		}
	}
	return out, nil
}

func key(tenantID, id string) string {
	return tenantID + ":" + id
}

func cloneTemplate(t model.Template) model.Template {
	t.Variables = append([]string(nil), t.Variables...)
	return t
}

func cloneMessage(m model.Message) model.Message {
	m.Attachments = append([]model.Attachment(nil), m.Attachments...)
	return m
}

func cloneOutbox(e model.OutboxEvent) model.OutboxEvent {
	if e.Payload != nil {
		cp := make(map[string]any, len(e.Payload))
		for k, v := range e.Payload {
			cp[k] = v
		}
		e.Payload = cp
	}
	return e
}
