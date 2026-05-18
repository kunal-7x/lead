//go:build integration

package internaladminapi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/internal-admin-api/internal/model"
	"github.com/lead/services/internal-admin-api/internal/service"
	"github.com/lead/services/internal-admin-api/internal/store"
)

func TestKillSwitchPausesProviderDrainsAndBlocksNewCalls(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	st.SetProviderInFlight("groq", 12)
	svc := service.New(st)
	actor := model.Actor{ID: "admin-1", Role: model.RoleSuperAdmin}

	killSwitch, err := svc.PauseProvider(ctx, actor, "groq", model.ActionRequest{Reason: "elevated 5xx", TicketID: "INC-77"})
	if err != nil {
		t.Fatalf("pause provider: %v", err)
	}
	if !killSwitch.Active || !killSwitch.Draining || killSwitch.DrainedCalls != 12 {
		t.Fatalf("unexpected kill switch: %#v", killSwitch)
	}
	health, ok, err := st.GetProvider(ctx, "groq")
	if err != nil || !ok {
		t.Fatalf("provider missing: %v, %v", ok, err)
	}
	if health.InFlightCalls != 0 || !health.CircuitOpen {
		t.Fatalf("provider did not drain/open circuit: %#v", health)
	}
	allowed, err := svc.CanStartProviderCall(ctx, "groq")
	if allowed || !errors.Is(err, service.ErrBlocked) {
		t.Fatalf("allowed=%v err=%v, want blocked", allowed, err)
	}
}
