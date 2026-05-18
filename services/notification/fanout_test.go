package notification_test

import (
	"context"
	"testing"

	"github.com/lead/services/notification/internal/adapters"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
	"github.com/lead/services/notification/internal/store"
)

func TestNotificationFanOutSendsEachExpectedChannelOnce(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	dashboard := adapters.NewDashboard()
	whatsapp := adapters.NewWhatsApp()
	email := adapters.NewEmail()
	webhook := adapters.NewWebhook()
	svc := service.New(st, map[model.Channel]adapters.Adapter{
		model.ChannelDashboard: dashboard,
		model.ChannelWhatsApp:  whatsapp,
		model.ChannelEmail:     email,
		model.ChannelSlack:     webhook,
		model.ChannelCRM:       webhook,
	})
	_, err := svc.Subscribe(ctx, model.Subscription{
		TenantID:   "tenant-1",
		EventTypes: []string{"handoff.created"},
		Channels: []model.Channel{
			model.ChannelDashboard,
			model.ChannelWhatsApp,
			model.ChannelEmail,
			model.ChannelSlack,
			model.ChannelCRM,
		},
		Recipients: []string{"rep-1", "+919876543210", "rep@example.com", "https://hooks.slack.test", "https://crm.test/hook"},
		Template:   "handoff-alert",
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	notifications, err := svc.FanOut(ctx, model.Event{
		TenantID: "tenant-1",
		Type:     "handoff.created",
		Payload:  map[string]any{"lead_id": "lead-1", "handoff_id": "handoff-1"},
	})
	if err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if len(notifications) != 5 {
		t.Fatalf("expected 5 notifications, got %d", len(notifications))
	}
	checkCount(t, "dashboard", dashboard.Count(model.ChannelDashboard), 1)
	checkCount(t, "whatsapp", whatsapp.Count(model.ChannelWhatsApp), 1)
	checkCount(t, "email", email.Count(model.ChannelEmail), 1)
	checkCount(t, "webhook slack", webhook.Count(model.ChannelSlack), 1)
	checkCount(t, "webhook crm", webhook.Count(model.ChannelCRM), 1)
}

func checkCount(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s count = %d, want %d", name, got, want)
	}
}
