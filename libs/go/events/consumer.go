package events

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Handler processes a single JetStream message. Return nil to ack, non-nil to
// nak so the message redelivers per the consumer policy.
type Handler func(ctx context.Context, subject string, data []byte, headers map[string][]string) error

// ConsumerConfig configures a durable JetStream consumer.
type ConsumerConfig struct {
	Stream         string
	Durable        string
	FilterSubject  string
	AckWait        time.Duration
	MaxDeliver     int
	DeliverPolicy  jetstream.DeliverPolicy
}

// Subscribe creates a durable pull consumer on the given stream and
// continuously consumes messages until ctx is cancelled.
func (p *JetStream) Subscribe(ctx context.Context, cfg ConsumerConfig, handler Handler) error {
	if cfg.AckWait == 0 {
		cfg.AckWait = 30 * time.Second
	}
	if cfg.MaxDeliver == 0 {
		cfg.MaxDeliver = 5
	}

	stream, err := p.js.Stream(ctx, cfg.Stream)
	if err != nil {
		return fmt.Errorf("events: stream %s: %w", cfg.Stream, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       cfg.Durable,
		FilterSubject: cfg.FilterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: cfg.DeliverPolicy,
		AckWait:       cfg.AckWait,
		MaxDeliver:    cfg.MaxDeliver,
	})
	if err != nil {
		return fmt.Errorf("events: consumer %s: %w", cfg.Durable, err)
	}

	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		hdrs := map[string][]string{}
		for k, v := range msg.Headers() {
			hdrs[k] = v
		}
		err := handler(ctx, msg.Subject(), msg.Data(), hdrs)
		if err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("events: consume %s: %w", cfg.Durable, err)
	}
	defer cc.Stop()

	<-ctx.Done()
	return nil
}
