package notification_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/notification/internal/adapters"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
	"github.com/lead/services/notification/internal/store"
)

func TestEachChannelAdapterRecordsContractSend(t *testing.T) {
	ctx := context.Background()
	for _, channel := range []model.Channel{
		model.ChannelDashboard,
		model.ChannelWhatsApp,
		model.ChannelEmail,
		model.ChannelSlack,
		model.ChannelDiscord,
		model.ChannelCRM,
	} {
		fake := adapters.NewFake()
		svc := service.New(store.NewFake(), map[model.Channel]adapters.Adapter{channel: fake})
		notification, err := svc.Send(ctx, model.SendRequest{
			TenantID:  "tenant-1",
			Channel:   channel,
			Recipient: "recipient",
			Template:  "template",
			Payload:   map[string]any{"handoff_id": "handoff-1"},
		})
		if err != nil {
			t.Fatalf("%s send: %v", channel, err)
		}
		if notification.Status != model.StatusSent || fake.Count(channel) != 1 {
			t.Fatalf("%s send not recorded: %#v count=%d", channel, notification, fake.Count(channel))
		}
	}
}

func TestFailedSendRecordsRetry(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	fake := adapters.NewFake()
	fake.NextErr = errors.New("temporary outage")
	svc := service.New(st, map[model.Channel]adapters.Adapter{model.ChannelEmail: fake})
	notification, err := svc.Send(ctx, model.SendRequest{
		TenantID:  "tenant-1",
		Channel:   model.ChannelEmail,
		Recipient: "rep@example.com",
		Template:  "handoff-alert",
		Payload:   map[string]any{"lead_id": "lead-1"},
	})
	if err == nil {
		t.Fatal("expected send failure")
	}
	if notification.Status != model.StatusFailed {
		t.Fatalf("expected failed notification, got %#v", notification)
	}
	retries, _ := st.ListRetries(ctx, "tenant-1")
	if len(retries) != 1 || retries[0].LastError == "" {
		t.Fatalf("expected one retry task, got %#v", retries)
	}
}
