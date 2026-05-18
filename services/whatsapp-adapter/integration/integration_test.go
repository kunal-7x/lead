//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func TestTemplateSyncAndSendRoundTripWithMetaMock(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	client := meta.NewFakeClient()
	client.RemoteTemplates = []meta.RemoteTemplate{
		{
			Name:     "site-visit-reminder",
			Language: "en",
			Category: model.TemplateCategoryUtility,
			Status:   "approved",
			Body:     "Reminder for {{visit_time}}",
			RemoteID: "tmpl-remote-1",
		},
	}
	svc := service.New(st, consent.StaticChecker{Allowed: true}, client)
	_, _ = svc.UpsertCredential(ctx, model.VaultCredential{
		TenantID:          "tenant-1",
		PhoneNumberID:     "phone-1",
		BusinessAccountID: "waba-1",
		AccessToken:       "token",
	})

	templates, err := svc.SyncTemplates(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(templates) != 1 || templates[0].RemoteID != "tmpl-remote-1" {
		t.Fatalf("unexpected synced templates: %#v", templates)
	}

	_, err = svc.SendTemplate(ctx, model.SendTemplateRequest{
		TenantID:   "tenant-1",
		LeadID:     "lead-1",
		Phone:      "+919876543210",
		TemplateID: templates[0].ID,
		Language:   "en",
		Variables:  map[string]string{"visit_time": "10:00"},
	})
	if err != nil {
		t.Fatalf("send template: %v", err)
	}
}
