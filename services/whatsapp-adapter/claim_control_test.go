package whatsappadapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lead/services/whatsapp-adapter/internal/claimcontrol"
	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

type blockingClaims struct{}

func (blockingClaims) Check(context.Context, claimcontrol.CheckRequest) (claimcontrol.CheckResponse, error) {
	return claimcontrol.CheckResponse{
		OK: false,
		Violations: []claimcontrol.Violation{{
			ClaimType: "appreciation",
			Reason:    "guaranteed appreciation is forbidden",
		}},
	}, nil
}

func TestSendMessageBlockedByClaimControl(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	client := meta.NewFakeClient()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, client)
	svc.SetClaimControl(blockingClaims{})

	thread, err := st.SaveThread(ctx, model.Thread{
		TenantID:      "tenant-1",
		LeadID:        "lead-1",
		Phone:         "+919876543210",
		LastInboundAt: time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	_, _ = svc.UpsertCredential(ctx, model.VaultCredential{TenantID: "tenant-1"})

	_, err = svc.SendMessage(ctx, model.SendMessageRequest{
		TenantID:  "tenant-1",
		ProjectID: "project-1",
		ThreadID:  thread.ID,
		Body:      "Guaranteed appreciation 20 percent yearly.",
	})
	if !errors.Is(err, service.ErrClaimBlocked) {
		t.Fatalf("err = %v, want ErrClaimBlocked", err)
	}
	if len(client.SentTexts) != 0 {
		t.Fatalf("meta send should not be called")
	}
}
