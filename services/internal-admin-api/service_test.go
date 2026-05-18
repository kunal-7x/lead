package internaladminapi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/internal-admin-api/internal/model"
	"github.com/lead/services/internal-admin-api/internal/service"
	"github.com/lead/services/internal-admin-api/internal/store"
)

func TestDestructiveTenantOpsRequireTicket(t *testing.T) {
	svc := service.New(store.NewFake())
	_, err := svc.DeleteTenant(context.Background(), model.Actor{ID: "admin-1", Role: model.RoleSuperAdmin}, "tenant-north", model.ActionRequest{Reason: "cleanup"})
	if !errors.Is(err, service.ErrTicketRequired) {
		t.Fatalf("err = %v, want ErrTicketRequired", err)
	}
}

func TestSupportSearchGatedToSuperAdmin(t *testing.T) {
	svc := service.New(store.NewFake())
	_, err := svc.SearchLeadByPhone(context.Background(), model.Actor{ID: "support-1", Role: model.RoleSupportAgent}, "9876543210")
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	results, err := svc.SearchLeadByPhone(context.Background(), model.Actor{ID: "admin-1", Role: model.RoleSuperAdmin}, "9876543210")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}
