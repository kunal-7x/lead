package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/cost"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/store"
	"github.com/lead/services/whatsapp-adapter/internal/templates"
	"github.com/lead/services/whatsapp-adapter/internal/webhook"
	"github.com/lead/services/whatsapp-adapter/internal/window"
)

var (
	ErrConsentBlocked       = errors.New("consent blocked")
	ErrInvalidSignature     = errors.New("invalid whatsapp webhook signature")
	ErrOptedOut             = errors.New("lead opted out of whatsapp")
	ErrOutsideServiceWindow = errors.New("outside 24h whatsapp service window")
)

type Service struct {
	store      store.Store
	consent    consent.Checker
	meta       meta.Client
	demoMeta   meta.Client
	demoTenant func(string) bool
	now        func() time.Time
}

func New(st store.Store, checker consent.Checker, client meta.Client) *Service {
	if checker == nil {
		checker = consent.StaticChecker{Allowed: true}
	}
	if client == nil {
		client = meta.NewFakeClient()
	}
	return &Service{
		store:      st,
		consent:    checker,
		meta:       client,
		demoMeta:   meta.NewDemoClient(),
		demoTenant: defaultDemoTenant,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) SetDemoMode(fn func(string) bool) {
	if fn != nil {
		s.demoTenant = fn
	}
}

func (s *Service) SetDemoClient(client meta.Client) {
	if client != nil {
		s.demoMeta = client
	}
}

func (s *Service) RegisterTemplate(ctx context.Context, tmpl model.Template) (model.Template, error) {
	if tmpl.TenantID == "" {
		return model.Template{}, errors.New("tenant_id is required")
	}
	if tmpl.Name == "" || tmpl.Language == "" {
		return model.Template{}, errors.New("template name and language are required")
	}
	if err := templates.ValidateCategory(tmpl.Category); err != nil {
		return model.Template{}, err
	}
	if tmpl.Status == "" {
		tmpl.Status = "draft"
	}
	if tmpl.MetaName == "" {
		tmpl.MetaName = tmpl.Name
	}
	return s.store.SaveTemplate(ctx, tmpl)
}

func (s *Service) SeedPrebuiltTemplates(ctx context.Context, tenantID string) error {
	for _, tmpl := range templates.Prebuilt(tenantID) {
		if _, err := s.RegisterTemplate(ctx, tmpl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SyncTemplates(ctx context.Context, tenantID string) ([]model.Template, error) {
	cred, err := s.store.GetCredential(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	remote, err := s.clientForTenant(tenantID).SyncTemplates(ctx, cred)
	if err != nil {
		s.enqueueRateLimitRetry(ctx, tenantID, "sync_templates", nil, err)
		return nil, err
	}
	out := make([]model.Template, 0, len(remote))
	for _, item := range remote {
		if err := templates.ValidateCategory(item.Category); err != nil {
			continue
		}
		existing, err := s.store.FindTemplateByName(ctx, tenantID, item.Name, item.Language)
		if err != nil {
			existing = model.Template{TenantID: tenantID, Name: item.Name, Language: item.Language}
		}
		now := s.now()
		existing.Category = item.Category
		existing.Status = item.Status
		existing.Body = item.Body
		existing.MetaName = item.Name
		existing.RemoteID = item.RemoteID
		existing.SyncedAt = &now
		saved, err := s.store.SaveTemplate(ctx, existing)
		if err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, nil
}

func (s *Service) ListTemplates(ctx context.Context, tenantID string) ([]model.Template, error) {
	return s.store.ListTemplates(ctx, tenantID)
}

func (s *Service) UpsertCredential(ctx context.Context, cred model.VaultCredential) (model.VaultCredential, error) {
	return s.store.UpsertCredential(ctx, cred)
}

func (s *Service) SendTemplate(ctx context.Context, req model.SendTemplateRequest) (model.Message, error) {
	tmpl, err := s.store.GetTemplate(ctx, req.TemplateID)
	if err != nil {
		return model.Message{}, err
	}
	if req.TenantID == "" {
		req.TenantID = tmpl.TenantID
	}
	if req.Language == "" {
		req.Language = tmpl.Language
	}
	if err := templates.ValidateCategory(tmpl.Category); err != nil {
		return model.Message{}, err
	}
	if err := s.ensureCanContact(ctx, req.TenantID, req.LeadID, req.Phone); err != nil {
		return model.Message{}, err
	}
	cred, err := s.store.GetCredential(ctx, req.TenantID)
	if err != nil {
		return model.Message{}, err
	}
	resp, err := s.clientForTenant(req.TenantID).SendTemplate(ctx, cred, meta.SendTemplateRequest{
		To:           req.Phone,
		TemplateName: tmpl.MetaName,
		Language:     req.Language,
		Variables:    req.Variables,
	})
	if err != nil {
		s.enqueueRateLimitRetry(ctx, req.TenantID, "send_template", map[string]any{
			"lead_id":     req.LeadID,
			"template_id": req.TemplateID,
		}, err)
		return model.Message{}, err
	}

	thread, err := s.threadForOutbound(ctx, req.TenantID, req.LeadID, req.Phone)
	if err != nil {
		return model.Message{}, err
	}
	msg, err := s.store.SaveMessage(ctx, model.Message{
		TenantID:      req.TenantID,
		ThreadID:      thread.ID,
		LeadID:        req.LeadID,
		Phone:         req.Phone,
		Direction:     model.DirectionOutbound,
		Kind:          model.MessageKindTemplate,
		Body:          tmpl.Body,
		TemplateID:    tmpl.ID,
		Status:        model.MessageStatusSent,
		MetaMessageID: resp.MessageID,
		CreatedAt:     s.now(),
	})
	if err != nil {
		return model.Message{}, err
	}
	unitCost, err := cost.TemplateCostINR(tmpl.Category)
	if err != nil {
		return model.Message{}, err
	}
	_, err = s.store.AddUsageEvent(ctx, model.UsageEvent{
		TenantID:     req.TenantID,
		LeadID:       req.LeadID,
		MessageID:    msg.ID,
		EventType:    "whatsapp_template_message",
		Quantity:     1,
		UnitCostINR:  unitCost,
		TotalCostINR: unitCost,
		CreatedAt:    s.now(),
	})
	return msg, err
}

func (s *Service) SendMessage(ctx context.Context, req model.SendMessageRequest) (model.Message, error) {
	thread, err := s.store.GetThread(ctx, req.ThreadID)
	if err != nil {
		return model.Message{}, err
	}
	if req.TenantID == "" {
		req.TenantID = thread.TenantID
	}
	if !window.IsOpen(s.now(), thread.LastInboundAt) {
		return model.Message{}, ErrOutsideServiceWindow
	}
	if err := s.ensureCanContact(ctx, req.TenantID, thread.LeadID, thread.Phone); err != nil {
		return model.Message{}, err
	}
	cred, err := s.store.GetCredential(ctx, req.TenantID)
	if err != nil {
		return model.Message{}, err
	}
	resp, err := s.clientForTenant(req.TenantID).SendText(ctx, cred, meta.SendTextRequest{To: thread.Phone, Body: req.Body})
	if err != nil {
		s.enqueueRateLimitRetry(ctx, req.TenantID, "send_message", map[string]any{"thread_id": req.ThreadID}, err)
		return model.Message{}, err
	}
	return s.store.SaveMessage(ctx, model.Message{
		TenantID:      req.TenantID,
		ThreadID:      thread.ID,
		LeadID:        thread.LeadID,
		Phone:         thread.Phone,
		Direction:     model.DirectionOutbound,
		Kind:          model.MessageKindText,
		Body:          req.Body,
		Attachments:   req.Attachments,
		Status:        model.MessageStatusSent,
		MetaMessageID: resp.MessageID,
		CreatedAt:     s.now(),
	})
}

func (s *Service) SendFlow(ctx context.Context, req model.SendFlowRequest) (model.Message, error) {
	if err := s.ensureCanContact(ctx, req.TenantID, req.LeadID, req.Phone); err != nil {
		return model.Message{}, err
	}
	cred, err := s.store.GetCredential(ctx, req.TenantID)
	if err != nil {
		return model.Message{}, err
	}
	resp, err := s.clientForTenant(req.TenantID).SendFlow(ctx, cred, meta.SendFlowRequest{To: req.Phone, FlowID: req.FlowID, Payload: req.Payload})
	if err != nil {
		s.enqueueRateLimitRetry(ctx, req.TenantID, "send_flow", map[string]any{"lead_id": req.LeadID, "flow_id": req.FlowID}, err)
		return model.Message{}, err
	}
	thread, err := s.threadForOutbound(ctx, req.TenantID, req.LeadID, req.Phone)
	if err != nil {
		return model.Message{}, err
	}
	return s.store.SaveMessage(ctx, model.Message{
		TenantID:      req.TenantID,
		ThreadID:      thread.ID,
		LeadID:        req.LeadID,
		Phone:         req.Phone,
		Direction:     model.DirectionOutbound,
		Kind:          model.MessageKindFlow,
		FlowID:        req.FlowID,
		Status:        model.MessageStatusSent,
		MetaMessageID: resp.MessageID,
		CreatedAt:     s.now(),
	})
}

func (s *Service) ProcessWebhook(ctx context.Context, tenantID string, body []byte, signature string) error {
	events, err := webhook.Normalize(body)
	if err != nil {
		return err
	}
	if tenantID == "" && len(events) > 0 {
		tenantID = events[0].TenantID
	}
	cred, err := s.store.GetCredential(ctx, tenantID)
	if err != nil {
		return err
	}
	if !meta.VerifySignature(cred.AppSecret, body, signature) {
		return ErrInvalidSignature
	}
	for _, event := range events {
		if event.TenantID == "" {
			event.TenantID = tenantID
		}
		created, err := s.store.RecordWebhookEvent(ctx, model.WebhookEvent{
			TenantID:       event.TenantID,
			IdempotencyKey: event.ID,
			EventType:      string(event.Type),
			Payload:        body,
			ReceivedAt:     s.now(),
		})
		if err != nil {
			return err
		}
		if !created {
			continue
		}
		if err := s.applyWebhookEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListThreads(ctx context.Context, tenantID string) ([]model.Thread, error) {
	return s.store.ListThreads(ctx, tenantID)
}

func (s *Service) ListMessages(ctx context.Context, threadID string) ([]model.Message, error) {
	return s.store.ListMessages(ctx, threadID)
}

func (s *Service) ensureCanContact(ctx context.Context, tenantID, leadID, phone string) error {
	optedOut, err := s.store.IsOptedOut(ctx, tenantID, leadID)
	if err != nil {
		return err
	}
	if optedOut {
		return ErrOptedOut
	}
	result, err := s.consent.CheckOutreachAllowed(ctx, consent.OutreachRequest{
		TenantID:      tenantID,
		LeadID:        leadID,
		Phone:         phone,
		Channel:       "whatsapp",
		RequestedTime: s.now(),
	})
	if err != nil {
		return err
	}
	if !result.Allowed {
		return fmt.Errorf("%w: %s", ErrConsentBlocked, result.Reason)
	}
	return nil
}

func (s *Service) clientForTenant(tenantID string) meta.Client {
	if s.demoTenant != nil && s.demoTenant(tenantID) {
		return s.demoMeta
	}
	return s.meta
}

func defaultDemoTenant(tenantID string) bool {
	if os.Getenv("DEMO_MODE") == "1" {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(tenantID))
	return normalized == "tenant-demo" ||
		normalized == "demo" ||
		strings.HasPrefix(normalized, "demo-") ||
		strings.HasSuffix(normalized, "-demo")
}

func (s *Service) threadForOutbound(ctx context.Context, tenantID, leadID, phone string) (model.Thread, error) {
	if leadID != "" {
		if thread, err := s.store.FindThreadByLead(ctx, tenantID, leadID); err == nil {
			if phone != "" && thread.Phone == "" {
				thread.Phone = phone
				return s.store.SaveThread(ctx, thread)
			}
			return thread, nil
		}
	}
	if phone != "" {
		if thread, err := s.store.FindThreadByPhone(ctx, tenantID, phone); err == nil {
			if leadID != "" && thread.LeadID == "" {
				thread.LeadID = leadID
				return s.store.SaveThread(ctx, thread)
			}
			return thread, nil
		}
	}
	return s.store.SaveThread(ctx, model.Thread{
		TenantID:  tenantID,
		LeadID:    leadID,
		Phone:     phone,
		CreatedAt: s.now(),
		UpdatedAt: s.now(),
	})
}

func (s *Service) applyWebhookEvent(ctx context.Context, event webhook.NormalizedEvent) error {
	switch event.Type {
	case webhook.EventReply, webhook.EventOptOut:
		thread, err := s.threadForInbound(ctx, event)
		if err != nil {
			return err
		}
		_, err = s.store.SaveMessage(ctx, model.Message{
			TenantID:      event.TenantID,
			ThreadID:      thread.ID,
			LeadID:        thread.LeadID,
			Phone:         thread.Phone,
			Direction:     model.DirectionInbound,
			Kind:          model.MessageKindText,
			Body:          event.Body,
			Status:        model.MessageStatusReceived,
			MetaMessageID: event.MessageID,
			CreatedAt:     event.Timestamp,
		})
		if err != nil {
			return err
		}
		_, err = s.store.AddOutboxEvent(ctx, model.OutboxEvent{
			TenantID: event.TenantID,
			Subject:  "whatsapp.reply",
			Type:     "whatsapp.reply",
			Payload: map[string]any{
				"lead_id":   thread.LeadID,
				"thread_id": thread.ID,
				"phone":     thread.Phone,
				"body":      event.Body,
			},
			CreatedAt: s.now(),
		})
		if err != nil {
			return err
		}
		if event.Type == webhook.EventOptOut {
			return s.recordOptOut(ctx, thread, event)
		}
	case webhook.EventRead, webhook.EventDelivery:
		_, err := s.store.AddOutboxEvent(ctx, model.OutboxEvent{
			TenantID: event.TenantID,
			Subject:  "whatsapp.status",
			Type:     string(event.Type),
			Payload: map[string]any{
				"message_id": event.MessageID,
				"status":     event.Status,
				"phone":      event.Phone,
			},
			CreatedAt: s.now(),
		})
		return err
	}
	return nil
}

func (s *Service) threadForInbound(ctx context.Context, event webhook.NormalizedEvent) (model.Thread, error) {
	thread, err := s.store.FindThreadByPhone(ctx, event.TenantID, event.Phone)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return model.Thread{}, err
	}
	if err == nil {
		thread.LastInboundAt = event.Timestamp
		thread.ServiceWindowUntil = window.Until(event.Timestamp)
		if event.LeadID != "" && thread.LeadID == "" {
			thread.LeadID = event.LeadID
		}
		return s.store.SaveThread(ctx, thread)
	}
	leadID := event.LeadID
	if leadID == "" {
		leadID = event.Phone
	}
	return s.store.SaveThread(ctx, model.Thread{
		TenantID:           event.TenantID,
		LeadID:             leadID,
		Phone:              event.Phone,
		LastInboundAt:      event.Timestamp,
		ServiceWindowUntil: window.Until(event.Timestamp),
		CreatedAt:          s.now(),
		UpdatedAt:          s.now(),
	})
}

func (s *Service) recordOptOut(ctx context.Context, thread model.Thread, event webhook.NormalizedEvent) error {
	if _, err := s.store.AddOptOut(ctx, model.WhatsAppOptOut{
		TenantID:  thread.TenantID,
		LeadID:    thread.LeadID,
		Phone:     thread.Phone,
		Channel:   "whatsapp",
		Reason:    event.Body,
		CreatedAt: s.now(),
	}); err != nil {
		return err
	}
	if _, err := s.store.AddConsentLedger(ctx, model.ConsentLedgerEntry{
		TenantID:  thread.TenantID,
		LeadID:    thread.LeadID,
		Channel:   "whatsapp",
		Basis:     "withdrawal",
		Source:    "whatsapp_reply",
		Reason:    event.Body,
		CreatedAt: s.now(),
	}); err != nil {
		return err
	}
	_, err := s.store.AddOutboxEvent(ctx, model.OutboxEvent{
		TenantID: thread.TenantID,
		Subject:  "tenant.inbox.notice",
		Type:     "whatsapp.opt_out",
		Payload: map[string]any{
			"lead_id":   thread.LeadID,
			"thread_id": thread.ID,
			"phone":     thread.Phone,
			"reason":    event.Body,
		},
		CreatedAt: s.now(),
	})
	return err
}

func (s *Service) enqueueRateLimitRetry(ctx context.Context, tenantID, operation string, payload map[string]any, err error) {
	var rateLimit *meta.RateLimitError
	if !errors.As(err, &rateLimit) {
		return
	}
	delay := meta.BackoffFromHeaders(rateLimit.Headers, 30*time.Second)
	_, _ = s.store.EnqueueRetry(ctx, model.RetryTask{
		TenantID:  tenantID,
		Operation: operation,
		Payload:   payload,
		RunAfter:  s.now().Add(delay),
		Attempts:  1,
		LastError: err.Error(),
		CreatedAt: s.now(),
	})
}
