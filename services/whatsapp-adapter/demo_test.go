//go:build demo

package whatsapp_adapter_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

type blockedMetaClient struct{}

func (blockedMetaClient) SendTemplate(context.Context, model.VaultCredential, meta.SendTemplateRequest) (meta.SendResponse, error) {
	return meta.SendResponse{}, errors.New("real meta client must not be called for demo tenant")
}

func (blockedMetaClient) SendText(context.Context, model.VaultCredential, meta.SendTextRequest) (meta.SendResponse, error) {
	return meta.SendResponse{}, errors.New("real meta client must not be called for demo tenant")
}

func (blockedMetaClient) SendFlow(context.Context, model.VaultCredential, meta.SendFlowRequest) (meta.SendResponse, error) {
	return meta.SendResponse{}, errors.New("real meta client must not be called for demo tenant")
}

func (blockedMetaClient) SyncTemplates(context.Context, model.VaultCredential) ([]meta.RemoteTemplate, error) {
	return nil, errors.New("real meta client must not be called for demo tenant")
}

func TestDemoTenantUsesMockWhatsAppAdapter(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, blockedMetaClient{})
	svc.SetDemoMode(func(tenantID string) bool { return tenantID == "tenant-demo" })

	tmpl, err := svc.RegisterTemplate(ctx, model.Template{
		TenantID: "tenant-demo",
		Name:     "demo_brochure",
		Language: "en_US",
		Category: model.TemplateCategoryMarketing,
		Body:     "Demo brochure",
		Status:   "APPROVED",
	})
	if err != nil {
		t.Fatalf("register template: %v", err)
	}
	if _, err := svc.UpsertCredential(ctx, model.VaultCredential{TenantID: "tenant-demo"}); err != nil {
		t.Fatalf("credential: %v", err)
	}

	msg, err := svc.SendTemplate(ctx, model.SendTemplateRequest{
		TenantID:   "tenant-demo",
		LeadID:     "lead-demo-1",
		Phone:      "+919876543210",
		TemplateID: tmpl.ID,
		Language:   "en_US",
	})
	if err != nil {
		t.Fatalf("send template: %v", err)
	}
	if !strings.HasPrefix(msg.MetaMessageID, "demo-wa-template-") {
		t.Fatalf("message id = %s, want demo-wa-template prefix", msg.MetaMessageID)
	}
	if msg.Status != model.MessageStatusSent {
		t.Fatalf("status = %s, want sent", msg.Status)
	}
}
