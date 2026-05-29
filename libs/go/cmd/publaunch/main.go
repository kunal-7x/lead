// Command publaunch publishes a campaign.launched event for live testing.
// Usage: go run ./cmd/publaunch <campaign_id>   (NATS_URL env or default)
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/lead/libs/go/events"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: publaunch <campaign_id>")
	}
	campaignID := os.Args[1]
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	pub, err := events.New(url)
	if err != nil {
		log.Fatalf("connect nats: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, _ := json.Marshal(map[string]string{"campaign_id": campaignID})
	if err := pub.Publish(ctx, events.SubjectCampaignLaunched, data, events.WithIdempotencyKey("launch-"+campaignID)); err != nil {
		log.Fatalf("publish: %v", err)
	}
	log.Printf("published campaign.launched campaign_id=%s", campaignID)
}
