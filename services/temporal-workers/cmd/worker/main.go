package main

import (
	"context"
	"log"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/lead/services/temporal-workers/internal/activities"
	"github.com/lead/services/temporal-workers/internal/workflows"
)

func main() {
	temporalHost := os.Getenv("TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	namespace := os.Getenv("TEMPORAL_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	c, err := client.Dial(client.Options{
		HostPort:  temporalHost,
		Namespace: namespace,
	})
	if err != nil {
		log.Fatalf("temporal-workers: dial %s: %v", temporalHost, err)
	}
	defer c.Close()

	w := worker.New(c, workflows.TaskQueue, worker.Options{})

	// Register workflows.
	w.RegisterWorkflow(workflows.RetryLadderWorkflow)
	w.RegisterWorkflow(workflows.CallingWindowGateWorkflow)
	w.RegisterWorkflow(workflows.CampaignHealthWorkflow)
	w.RegisterWorkflow(workflows.CostCapWatchWorkflow)

	// Register activities.
	w.RegisterActivity(activities.PickNextCallActivity)
	w.RegisterActivity(activities.SignalSchedulerActivity)
	w.RegisterActivity(activities.CheckCallingWindowActivity)
	w.RegisterActivity(activities.RecordCostActivity)
	w.RegisterActivity(activities.PauseCampaignActivity)
	w.RegisterActivity(activities.GetCampaignHealthActivity)

	log.Printf("temporal-workers: connected to %s namespace=%s queue=%s", temporalHost, namespace, workflows.TaskQueue)
	log.Printf("temporal-workers: workflows: RetryLadderWorkflow, CallingWindowGateWorkflow, CampaignHealthWorkflow, CostCapWatchWorkflow")

	// Start NATS trigger goroutine — subscribes to campaign.launched and starts
	// CampaignHealthWorkflow for each new campaign.
	ctx := context.Background()
	go startNATSTrigger(ctx, c)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("temporal-workers: run: %v", err)
	}
}
