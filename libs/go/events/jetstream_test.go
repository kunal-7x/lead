package events_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lead/libs/go/events"
)

// natsURL returns the NATS URL from env, or "" to skip live tests.
func natsURL() string {
	if u := os.Getenv("NATS_TEST_URL"); u != "" {
		return u
	}
	if u := os.Getenv("NATS_URL"); u != "" {
		return u
	}
	return ""
}

func TestNew_NoURL(t *testing.T) {
	if _, err := events.New(""); err != events.ErrNoURL {
		t.Fatalf("expected ErrNoURL, got %v", err)
	}
}

func TestNoopPublisher_Records(t *testing.T) {
	p := events.NewNoop()
	ctx := context.Background()
	if err := p.Publish(ctx, "lead.scored", []byte(`{"x":1}`), events.WithIdempotencyKey("k1")); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(ctx, "lead.scored", []byte(`{"x":2}`)); err != nil {
		t.Fatal(err)
	}
	if got := p.Count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	msgs := p.Messages()
	if msgs[0].Subject != "lead.scored" {
		t.Fatalf("subject = %s", msgs[0].Subject)
	}
	if got := msgs[0].Headers["Nats-Msg-Id"]; len(got) != 1 || got[0] != "k1" {
		t.Fatalf("idempotency header missing: %v", msgs[0].Headers)
	}
}

func TestNewFromEnv_EmptyReturnsNoop(t *testing.T) {
	p, err := events.NewFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*events.NoopPublisher); !ok {
		t.Fatalf("expected NoopPublisher, got %T", p)
	}
}

func TestPublishToJetStream(t *testing.T) {
	url := natsURL()
	if url == "" {
		t.Skip("set NATS_TEST_URL to run JetStream integration test")
	}
	p, err := events.New(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	subj := events.SubjectLeadScored
	if err := p.Publish(ctx, subj, []byte(`{"lead":"l1","score":42}`), events.WithIdempotencyKey("test-key-1")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Republish with same idempotency key — JetStream should dedupe.
	if err := p.Publish(ctx, subj, []byte(`{"lead":"l1","score":42}`), events.WithIdempotencyKey("test-key-1")); err != nil {
		t.Fatalf("publish dup: %v", err)
	}
}

func TestSubscribeRoundTrip(t *testing.T) {
	url := natsURL()
	if url == "" {
		t.Skip("set NATS_TEST_URL to run JetStream integration test")
	}
	p, err := events.New(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan string, 1)
	go func() {
		_ = p.Subscribe(ctx, events.ConsumerConfig{
			Stream:        "CAPSY_LEAD",
			Durable:       "test-rt-consumer",
			FilterSubject: events.SubjectLeadScored,
		}, func(_ context.Context, subject string, data []byte, _ map[string][]string) error {
			got <- subject + ":" + string(data)
			return nil
		})
	}()

	time.Sleep(200 * time.Millisecond) // let consumer subscribe
	if err := p.Publish(ctx, events.SubjectLeadScored, []byte("rt")); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-got:
		if msg != events.SubjectLeadScored+":rt" {
			t.Fatalf("got %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}
