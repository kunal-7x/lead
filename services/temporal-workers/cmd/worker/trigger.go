package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/temporal-workers/internal/model"
	"github.com/lead/services/temporal-workers/internal/workflows"
)

// startNATSTrigger subscribes to campaign.launched and starts CampaignHealthWorkflow
// for each launched campaign. No-op when NATS_URL is unset.
func startNATSTrigger(ctx context.Context, tc client.Client) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		return
	}
	p, err := events.New(natsURL)
	if err != nil {
		log.Printf("temporal-workers trigger: nats connect: %v", err)
		return
	}
	defer p.Close()
	log.Printf("temporal-workers trigger: subscribed to %s", events.SubjectCampaignLaunched)

	_ = p.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CAMPAIGN",
		Durable:       "temporal-workers-campaign-launched",
		FilterSubject: events.SubjectCampaignLaunched,
	}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
		var ev struct {
			CampaignID string `json:"campaign_id"`
			TenantID   string `json:"tenant_id"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			log.Printf("temporal-workers trigger: parse: %v", err)
			return nil
		}
		if ev.CampaignID == "" {
			return nil
		}
		return startCampaignHealthWorkflow(ctx, tc, ev.CampaignID)
	})
}

func startCampaignHealthWorkflow(ctx context.Context, tc client.Client, campaignID string) error {
	opts := client.StartWorkflowOptions{
		ID:        "campaign-health-" + campaignID,
		TaskQueue: workflows.TaskQueue,
		// Reuse existing workflow if it's already running for this campaign.
		WorkflowIDReusePolicy: 2, // AllowDuplicateFailedOnly → prevents double-start
	}
	input := model.CampaignHealthInput{
		CampaignID:      campaignID,
		IntervalSeconds: 60,
	}

	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	we, err := tc.ExecuteWorkflow(ctx2, opts, workflows.CampaignHealthWorkflow, input)
	if err != nil {
		log.Printf("temporal-workers: start CampaignHealthWorkflow %s: %v", campaignID, err)
		return nil // don't redeliver — Temporal will retry startup
	}
	log.Printf("temporal-workers: started CampaignHealthWorkflow %s run=%s", campaignID, we.GetRunID())
	return nil
}
