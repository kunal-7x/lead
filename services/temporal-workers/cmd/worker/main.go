package main

import (
	"log"
	"os"
)

// main is the Temporal worker entrypoint.
// In production, this registers all workflows and activities with a Temporal client.
// Requires a running Temporal server (see PORT_MAP.md: port 7233).
func main() {
	temporalHost := os.Getenv("TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}

	log.Printf("temporal-workers starting; connecting to %s", temporalHost)
	log.Printf("workflows: RetryLadderWorkflow, CallingWindowGate, CostCapWatchWorkflow, CampaignHealthWorkflow")
	log.Printf("NOTE: set TEMPORAL_HOST env var for non-default server address")

	// Production registration would be:
	//   c, _ := client.Dial(client.Options{HostPort: temporalHost})
	//   w := worker.New(c, "lead-scheduler-task-queue", worker.Options{})
	//   w.RegisterWorkflow(workflows.RetryLadderWorkflow)
	//   w.RegisterWorkflow(workflows.CallingWindowGate)
	//   w.RegisterWorkflow(workflows.CostCapWatchWorkflow)
	//   w.RegisterWorkflow(workflows.CampaignHealthWorkflow)
	//   if err := w.Run(worker.InterruptCh()); err != nil { log.Fatal(err) }
	log.Fatal("temporal server not available in this environment; see HUMAN_TASKS.md")
}
