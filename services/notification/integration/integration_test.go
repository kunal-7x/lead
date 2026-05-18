//go:build integration && temporal

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/lead/services/notification/internal/adapters"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
	"github.com/lead/services/notification/internal/store"
)

func TestTemporalRetryRecordedForFanOutFailure(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	email := adapters.NewFake()
	email.NextErr = errors.New("postmark timeout")
	svc := service.New(st, map[model.Channel]adapters.Adapter{model.ChannelEmail: email})
	_, _ = svc.Subscribe(ctx, model.Subscription{
		TenantID:   "tenant-1",
		EventTypes: []string{"handoff.created"},
		Channels:   []model.Channel{model.ChannelEmail},
		Recipients: []string{"rep@example.com"},
		Template:   "handoff-alert",
	})
	_, err := svc.FanOut(ctx, model.Event{
		TenantID: "tenant-1",
		Type:     "handoff.created",
		Payload:  map[string]any{"handoff_id": "handoff-1"},
	})
	if err == nil {
		t.Fatal("expected fanout failure")
	}
	retries, _ := st.ListRetries(ctx, "tenant-1")
	if len(retries) != 1 || retries[0].Channel != model.ChannelEmail {
		t.Fatalf("expected email retry task, got %#v", retries)
	}
}
