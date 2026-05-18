package whatsappadapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func TestWebhookSignatureRejectsTamperedPayload(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, meta.NewFakeClient())
	_, err := svc.UpsertCredential(ctx, model.VaultCredential{
		TenantID:  "tenant-1",
		AppSecret: "top-secret",
	})
	if err != nil {
		t.Fatalf("credential: %v", err)
	}

	original := []byte(`{"tenant_id":"tenant-1","events":[{"id":"evt-1","type":"reply","lead_id":"lead-1","phone":"+919876543210","body":"hello"}]}`)
	tampered := []byte(`{"tenant_id":"tenant-1","events":[{"id":"evt-1","type":"reply","lead_id":"lead-1","phone":"+919876543210","body":"changed"}]}`)

	err = svc.ProcessWebhook(ctx, "tenant-1", tampered, meta.Signature("top-secret", original))
	if !errors.Is(err, service.ErrInvalidSignature) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
}
