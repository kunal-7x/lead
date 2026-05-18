//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/internal-admin-api/internal/model"
	"github.com/lead/services/internal-admin-api/internal/service"
	"github.com/lead/services/internal-admin-api/internal/store"
)

func TestDestructiveOpsRequireTicketID(t *testing.T) {
	svc := service.New(store.NewFake())
	actor := model.Actor{ID: "admin-1", Role: model.RoleSuperAdmin}
	_, err := svc.SuspendTenant(context.Background(), actor, "tenant-north", model.ActionRequest{Reason: "manual pause"})
	if !errors.Is(err, service.ErrTicketRequired) {
		t.Fatalf("err = %v, want ErrTicketRequired", err)
	}
}

func TestCrossTenantLeadSearchRequiresSuperAdmin(t *testing.T) {
	svc := service.New(store.NewFake())
	_, err := svc.SearchLeadByPhone(context.Background(), model.Actor{ID: "ops-1", Role: model.RoleOpsAdmin}, "9876543210")
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}
