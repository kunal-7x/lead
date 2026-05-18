package whatsappadapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func TestOptOutReplyAddsSuppressionLedgerAndInboxNotice(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, meta.NewFakeClient())
	_, _ = svc.UpsertCredential(ctx, model.VaultCredential{TenantID: "tenant-1"})
	_, err := st.SaveThread(ctx, model.Thread{
		TenantID:      "tenant-1",
		LeadID:        "lead-1",
		Phone:         "+919876543210",
		LastInboundAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed thread: %v", err)
	}

	body := []byte(`{"tenant_id":"tenant-1","events":[{"id":"evt-stop-1","type":"reply","lead_id":"lead-1","phone":"+919876543210","body":"STOP","timestamp":"2026-05-18T06:00:00Z"}]}`)
	if err := svc.ProcessWebhook(ctx, "tenant-1", body, ""); err != nil {
		t.Fatalf("webhook: %v", err)
	}

	optedOut, err := st.IsOptedOut(ctx, "tenant-1", "lead-1")
	if err != nil {
		t.Fatalf("opt-out lookup: %v", err)
	}
	if !optedOut {
		t.Fatal("expected lead to be opted out")
	}
	ledger, err := st.ListConsentLedger(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if len(ledger) != 1 || ledger[0].Basis != "withdrawal" || ledger[0].Channel != "whatsapp" {
		t.Fatalf("expected one whatsapp withdrawal ledger row, got %#v", ledger)
	}
	outbox, err := st.ListOutboxEvents(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	foundNotice := false
	for _, event := range outbox {
		if event.Type == "whatsapp.opt_out" && event.Subject == "tenant.inbox.notice" {
			foundNotice = true
		}
	}
	if !foundNotice {
		t.Fatalf("expected tenant inbox opt-out notice, got %#v", outbox)
	}
}
