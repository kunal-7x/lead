package dispatcher

import (
	"context"
	"encoding/json"

	"github.com/lead/libs/go/events"
)

// NATSSubscriber wraps events.JetStream to implement the Subscriber interface.
type NATSSubscriber struct {
	js *events.JetStream
}

// NewNATSSubscriber creates a subscriber backed by a live JetStream connection.
func NewNATSSubscriber(js *events.JetStream) *NATSSubscriber {
	return &NATSSubscriber{js: js}
}

// SubscribeCampaignLaunched subscribes to campaign.launched and calls fn for
// each message's campaign_id. Blocks until ctx is cancelled.
func (n *NATSSubscriber) SubscribeCampaignLaunched(ctx context.Context, fn func(campaignID string)) error {
	return n.js.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CAMPAIGN",
		Durable:       "scheduler-dispatcher",
		FilterSubject: string(events.SubjectCampaignLaunched),
	}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
		var payload struct {
			CampaignID string `json:"campaign_id"`
		}
		if err := json.Unmarshal(data, &payload); err != nil || payload.CampaignID == "" {
			return nil // ack and discard malformed messages
		}
		fn(payload.CampaignID)
		return nil
	})
}
