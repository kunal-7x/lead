package routing_test

import (
	"context"
	"testing"

	"github.com/lead/services/telephony-adapter/internal/adapter"
	"github.com/lead/services/telephony-adapter/internal/esl"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/store"
)

func buildStore(rules []model.ProviderRoutingRule) *store.Fake {
	s := store.NewFake()
	s.ResetRoutingRules()
	for _, r := range rules {
		s.AddRoutingRule(r)
	}
	return s
}

func TestFreeSWITCHFailover(t *testing.T) {
	// Set up: FreeSWITCH (priority 1) → Plivo mock (priority 2).
	eslFake := esl.NewFakeClient()
	eslFake.SetHealthy(false) // FreeSWITCH is down

	fsAdapter := adapter.NewFreeSWITCH(eslFake, "/tmp/recordings", "ws://voice-agent:8765/audio")
	plivoMock := adapter.NewMock()

	rules := []model.ProviderRoutingRule{
		{ID: "r1", ProviderID: "freeswitch", Priority: 1, MaxFailRate: 0.5},
		{ID: "r2", ProviderID: "mock", Priority: 2, MaxFailRate: 0.5},
	}

	s := buildStore(rules)
	router := routing.New(s, []adapter.Telephony{fsAdapter, plivoMock})

	selected, err := router.Select(context.Background(), "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selected.Name() != "mock" {
		t.Errorf("expected fallback to mock, got %q", selected.Name())
	}
}

func TestFreeSWITCHHealthy_SelectedFirst(t *testing.T) {
	eslFake := esl.NewFakeClient()
	eslFake.SetHealthy(true) // FreeSWITCH is up

	fsAdapter := adapter.NewFreeSWITCH(eslFake, "/tmp/recordings", "ws://voice-agent:8765/audio")
	plivoMock := adapter.NewMock()

	rules := []model.ProviderRoutingRule{
		{ID: "r1", ProviderID: "freeswitch", Priority: 1, MaxFailRate: 0.5},
		{ID: "r2", ProviderID: "mock", Priority: 2, MaxFailRate: 0.5},
	}

	s := buildStore(rules)
	router := routing.New(s, []adapter.Telephony{fsAdapter, plivoMock})

	selected, err := router.Select(context.Background(), "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selected.Name() != "freeswitch" {
		t.Errorf("expected freeswitch selected first, got %q", selected.Name())
	}
}
