//go:build demo

package telephony_adapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
)

func TestDemoTenantUsesMockAdapter(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	plivo := adapter.NewMock()
	mock := adapter.NewMock()
	mock.SetScriptedStates("queued", "ringing", "answered", "completed")
	mock.SetAudioLoop([]byte{1, 2}, []byte{3, 4})
	router := routing.New(st, []adapter.Telephony{
		&demoNamedAdapter{Mock: plivo, name: "plivo"},
		&demoNamedAdapter{Mock: mock, name: "mock"},
	})

	selected, err := router.SelectForCall(ctx, model.CallRequest{TenantID: "tenant-demo", Demo: true})
	if err != nil {
		t.Fatalf("select demo: %v", err)
	}
	if selected.Name() != "mock" {
		t.Fatalf("provider = %s, want mock", selected.Name())
	}
	callID, err := selected.PlaceCall(ctx, model.CallRequest{TenantID: "tenant-demo", ToNumber: "+919876543210", Demo: true})
	if err != nil {
		t.Fatalf("place demo call: %v", err)
	}
	if !strings.HasPrefix(string(callID), "mock-call-") {
		t.Fatalf("call id = %s, want mock-call prefix", callID)
	}
	if len(mock.ScriptedStates()) != 4 || len(mock.AudioLoop()) != 2 {
		t.Fatalf("demo script not configured")
	}
}

func TestDemoTenantCannotUseRealProviderWithoutMock(t *testing.T) {
	router := routing.New(store.NewFake(), []adapter.Telephony{
		&demoNamedAdapter{Mock: adapter.NewMock(), name: "plivo"},
	})
	_, err := router.SelectForCall(context.Background(), model.CallRequest{TenantID: "tenant-demo", Demo: true})
	if err == nil {
		t.Fatal("expected demo tenant to be blocked when mock provider is missing")
	}
}

type demoNamedAdapter struct {
	*adapter.Mock
	name string
}

func (n *demoNamedAdapter) Name() string { return n.name }
