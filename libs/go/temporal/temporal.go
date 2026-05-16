package temporal

import (
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Config holds Temporal connection settings.
type Config struct {
	HostPort  string // e.g. "localhost:7233"
	Namespace string
}

// NewClient creates a Temporal client.
func NewClient(cfg Config) (client.Client, error) {
	c, err := client.Dial(client.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("temporal dial: %w", err)
	}
	return c, nil
}

// NewWorker creates a task-queue worker attached to the given client.
func NewWorker(c client.Client, taskQueue string, opts worker.Options) worker.Worker {
	return worker.New(c, taskQueue, opts)
}
