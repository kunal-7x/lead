//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"log/slog"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestNATSConsumerWritesClickHouse(t *testing.T) {
	natsURL := os.Getenv("NATS_TEST_URL")
	if natsURL == "" {
		natsURL = os.Getenv("NATS_URL")
	}
	clickhouseURL := os.Getenv("CLICKHOUSE_URL")
	if natsURL == "" || clickhouseURL == "" {
		t.Skip("set NATS_TEST_URL and CLICKHOUSE_URL to run NATS to ClickHouse integration test")
	}
	t.Setenv("NATS_URL", natsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := store.NewClickHouse(clickhouseURL)
	if err != nil {
		t.Fatalf("new clickhouse store: %v", err)
	}
	defer st.Close()
	svc := service.New(st)

	done := make(chan struct{})
	go func() {
		startConsumer(ctx, slog.New(slog.NewTextHandler(os.Stdout, nil)), svc)
		close(done)
	}()

	pub, err := events.New(natsURL)
	if err != nil {
		t.Fatalf("new nats publisher: %v", err)
	}
	defer pub.Close()

	suffix := time.Now().Format("20060102150405")
	tenantID := "tenant-c11-nats-" + suffix
	eventID := "evt-c11-nats-" + suffix
	payload, _ := json.Marshal(map[string]any{
		"event_id":         eventID,
		"tenant_id":        tenantID,
		"campaign_id":      "campaign-1",
		"duration_seconds": 33,
	})
	time.Sleep(500 * time.Millisecond)
	if err := pub.Publish(ctx, events.SubjectCallCompleted, payload, events.WithIdempotencyKey(eventID)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.After(10 * time.Second)
	for {
		count, err := st.CountFact(ctx, tenantID, "fact_calls")
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if count == 1 {
			cancel()
			<-done
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for fact_calls row")
		case <-time.After(250 * time.Millisecond):
		}
	}
}
