package whatsappadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func TestSeedDemoInboxCreatesAPIBrowsableThreadsAndMessages(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, meta.NewFakeClient())
	now := time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC)
	svc.SetNow(func() time.Time { return now })

	if err := svc.SeedDemoInbox(ctx, "tenant-demo"); err != nil {
		t.Fatalf("seed demo inbox: %v", err)
	}

	threads, err := svc.ListThreads(ctx, "tenant-demo")
	if err != nil {
		t.Fatalf("list threads: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("expected 3 demo threads, got %d: %#v", len(threads), threads)
	}

	visit := requireThread(t, threads, "lead-demo-002")
	visitMessages, err := svc.ListMessages(ctx, visit.ID)
	if err != nil {
		t.Fatalf("list visit messages: %v", err)
	}
	if len(visitMessages) != 2 || visitMessages[0].Body != "Can I visit on Saturday?" {
		t.Fatalf("unexpected visit messages: %#v", visitMessages)
	}

	reply, err := svc.SendMessage(ctx, model.SendMessageRequest{
		TenantID: "tenant-demo",
		ThreadID: visit.ID,
		Body:     "Yes, I can hold the 4 PM slot.",
	})
	if err != nil {
		t.Fatalf("send demo reply: %v", err)
	}
	if !strings.HasPrefix(reply.MetaMessageID, "demo-wa-text-") {
		t.Fatalf("expected demo meta text id, got %q", reply.MetaMessageID)
	}

	optOut := requireThread(t, threads, "lead-demo-001")
	_, err = svc.SendMessage(ctx, model.SendMessageRequest{
		TenantID: "tenant-demo",
		ThreadID: optOut.ID,
		Body:     "Following up",
	})
	if !errors.Is(err, service.ErrOptedOut) {
		t.Fatalf("expected opt-out error, got %v", err)
	}

	ledger, err := st.ListConsentLedger(ctx, "tenant-demo")
	if err != nil {
		t.Fatalf("list consent ledger: %v", err)
	}
	if len(ledger) != 1 || ledger[0].LeadID != "lead-demo-001" || ledger[0].Basis != "withdrawal" {
		t.Fatalf("unexpected demo consent ledger: %#v", ledger)
	}
}

func TestSendMessageRequestAcceptsDashboardJSONShape(t *testing.T) {
	var req model.SendMessageRequest
	if err := json.Unmarshal(
		[]byte(`{"tenant_id":"tenant-demo","thread_id":"wa-thread-demo-visit","body":"Hello"}`),
		&req,
	); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.TenantID != "tenant-demo" || req.ThreadID != "wa-thread-demo-visit" || req.Body != "Hello" {
		t.Fatalf("unexpected decoded request: %#v", req)
	}
}

func requireThread(t *testing.T, threads []model.Thread, leadID string) model.Thread {
	t.Helper()
	for _, thread := range threads {
		if thread.LeadID == leadID {
			return thread
		}
	}
	t.Fatalf("missing thread for lead %s in %#v", leadID, threads)
	return model.Thread{}
}
