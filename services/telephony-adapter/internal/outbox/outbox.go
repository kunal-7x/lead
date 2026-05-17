// Package outbox provides event publishing with NATS-like semantics.
package outbox

import (
	"context"
	"sync"
)

// Publisher emits events to a message bus.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// Message is a published event.
type Message struct {
	Subject string
	Data    []byte
}

// FakePublisher records published messages for testing.
type FakePublisher struct {
	mu       sync.Mutex
	messages []Message
}

func NewFakePublisher() *FakePublisher {
	return &FakePublisher{}
}

func (p *FakePublisher) Publish(_ context.Context, subject string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, Message{Subject: subject, Data: data})
	return nil
}

func (p *FakePublisher) Messages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Message, len(p.messages))
	copy(out, p.messages)
	return out
}

func (p *FakePublisher) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.messages)
}
