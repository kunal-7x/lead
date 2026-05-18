package whatsappadapter_test

import (
	"context"
	"testing"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func TestTemplateSendEmitsOneUsageEventPerMessage(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, meta.NewFakeClient())
	_, _ = svc.UpsertCredential(ctx, model.VaultCredential{TenantID: "tenant-1", PhoneNumberID: "phone-1"})
	tmpl, err := svc.RegisterTemplate(ctx, model.Template{
		TenantID: "tenant-1",
		Name:     "lead-warm-up",
		Language: "en",
		Category: model.TemplateCategoryMarketing,
		Body:     "Hello {{name}}",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("template: %v", err)
	}

	for i := 0; i < 100; i++ {
		_, err := svc.SendTemplate(ctx, model.SendTemplateRequest{
			TenantID:   "tenant-1",
			LeadID:     "lead-1",
			Phone:      "+919876543210",
			TemplateID: tmpl.ID,
			Language:   "en",
			Variables:  map[string]string{"name": "Asha"},
		})
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	events, err := st.ListUsageEvents(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("usage events: %v", err)
	}
	if len(events) != 100 {
		t.Fatalf("expected 100 usage_event rows, got %d", len(events))
	}
	for _, event := range events {
		if event.EventType != "whatsapp_template_message" || event.TotalCostINR <= 0 {
			t.Fatalf("bad usage event: %#v", event)
		}
	}
}
