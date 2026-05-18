package whatsappadapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lead/services/whatsapp-adapter/internal/consent"
	"github.com/lead/services/whatsapp-adapter/internal/meta"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
	"github.com/lead/services/whatsapp-adapter/internal/store"
)

func TestOutside24HourWindowRequiresTemplate(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st, consent.StaticChecker{Allowed: true}, meta.NewFakeClient())
	now := time.Date(2026, 5, 18, 6, 0, 0, 0, time.UTC)
	svc.SetNow(func() time.Time { return now })

	thread, err := st.SaveThread(ctx, model.Thread{
		TenantID:      "tenant-1",
		LeadID:        "lead-1",
		Phone:         "+919876543210",
		LastInboundAt: now.Add(-25 * time.Hour),
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	_, err = svc.SendMessage(ctx, model.SendMessageRequest{
		TenantID: "tenant-1",
		ThreadID: thread.ID,
		Body:     "plain text outside the service window",
	})
	if !errors.Is(err, service.ErrOutsideServiceWindow) {
		t.Fatalf("expected outside service window error, got %v", err)
	}
}
