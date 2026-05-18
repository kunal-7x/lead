package internaladminapi_test

import (
	"context"
	"testing"

	"github.com/lead/services/internal-admin-api/internal/model"
	"github.com/lead/services/internal-admin-api/internal/service"
	"github.com/lead/services/internal-admin-api/internal/store"
)

func TestImpersonationAuditRecordsActorAndTarget(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	actor := model.Actor{ID: "admin-7", Role: model.RoleSuperAdmin}

	token, err := svc.ImpersonateTenant(ctx, actor, "tenant-north", model.ActionRequest{Reason: "support escalation", TicketID: "SUP-123"})
	if err != nil {
		t.Fatalf("impersonate: %v", err)
	}
	if token == "" {
		t.Fatal("expected impersonation token")
	}
	entries, err := st.ListAudit(ctx, model.AuditFilter{Action: "tenant.impersonate"})
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ActorID != "admin-7" || entry.TargetID != "tenant-north" || entry.Metadata["target_tenant_id"] != "tenant-north" {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
}
