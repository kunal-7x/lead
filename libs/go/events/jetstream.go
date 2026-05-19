package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// PublishOption customizes a single Publish call.
type PublishOption func(*publishOptions)

type publishOptions struct {
	idempotencyKey string
	headers        nats.Header
}

// WithIdempotencyKey sets the "Nats-Msg-Id" header, which JetStream uses for
// deduplication within the stream's deduplication window (default 2 minutes).
// Provide the outbox row id or a deterministic event id.
func WithIdempotencyKey(key string) PublishOption {
	return func(o *publishOptions) {
		o.idempotencyKey = key
	}
}

// WithHeader adds an arbitrary header to the message.
func WithHeader(name, value string) PublishOption {
	return func(o *publishOptions) {
		if o.headers == nil {
			o.headers = nats.Header{}
		}
		o.headers.Add(name, value)
	}
}

// Publisher abstracts a JetStream publisher. Production code uses *JetStream;
// tests use NoopPublisher.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error
	Close() error
}

// JetStream is the production Publisher backed by NATS JetStream.
type JetStream struct {
	nc *nats.Conn
	js jetstream.JetStream

	mu      sync.Mutex
	streams map[string]bool
}

// New connects to NATS at url and asserts the DefaultStreams exist.
// If url is empty, returns ErrNoURL.
func New(url string) (*JetStream, error) {
	if url == "" {
		return nil, ErrNoURL
	}
	return NewWithStreams(url, DefaultStreams)
}

// NewWithStreams connects and asserts the given stream specs.
func NewWithStreams(url string, streams []StreamSpec) (*JetStream, error) {
	nc, err := nats.Connect(url,
		nats.Name("capsy"),
		nats.Timeout(5*time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("events: nats connect %s: %w", url, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("events: jetstream init: %w", err)
	}

	p := &JetStream{nc: nc, js: js, streams: map[string]bool{}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range streams {
		if err := p.assertStream(ctx, s); err != nil {
			nc.Close()
			return nil, err
		}
	}

	return p, nil
}

// ErrNoURL is returned by New when the URL is empty.
var ErrNoURL = errors.New("events: NATS_URL not set")

func (p *JetStream) assertStream(ctx context.Context, spec StreamSpec) error {
	cfg := jetstream.StreamConfig{
		Name:       spec.Name,
		Subjects:   spec.Subjects,
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     7 * 24 * time.Hour,
		Duplicates: 2 * time.Minute,
		Replicas:   1,
	}
	_, err := p.js.CreateStream(ctx, cfg)
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
			if _, uerr := p.js.UpdateStream(ctx, cfg); uerr != nil {
				return fmt.Errorf("events: update stream %s: %w", spec.Name, uerr)
			}
		} else {
			return fmt.Errorf("events: create stream %s: %w", spec.Name, err)
		}
	}
	p.mu.Lock()
	p.streams[spec.Name] = true
	p.mu.Unlock()
	return nil
}

// Publish publishes data to subject with optional headers/idempotency key.
func (p *JetStream) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	o := &publishOptions{}
	for _, opt := range opts {
		opt(o)
	}
	msg := &nats.Msg{Subject: subject, Data: data, Header: o.headers}
	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	if o.idempotencyKey != "" {
		msg.Header.Set(nats.MsgIdHdr, o.idempotencyKey)
	}
	if _, err := p.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("events: publish %s: %w", subject, err)
	}
	return nil
}

// Conn exposes the underlying NATS connection for consumers.
func (p *JetStream) Conn() *nats.Conn { return p.nc }

// JS exposes the underlying JetStream context for consumers.
func (p *JetStream) JS() jetstream.JetStream { return p.js }

// Close drains and closes the NATS connection.
func (p *JetStream) Close() error {
	if p.nc == nil {
		return nil
	}
	return p.nc.Drain()
}

// NoopPublisher discards every message. Used in tests and when NATS is not
// configured.
type NoopPublisher struct {
	mu       sync.Mutex
	messages []Message
}

// Message captures a single Publish call.
type Message struct {
	Subject string
	Data    []byte
	Headers map[string][]string
}

// NewNoop returns a NoopPublisher that records messages for inspection.
func NewNoop() *NoopPublisher { return &NoopPublisher{} }

// Publish records the message and returns nil.
func (p *NoopPublisher) Publish(_ context.Context, subject string, data []byte, opts ...PublishOption) error {
	o := &publishOptions{}
	for _, opt := range opts {
		opt(o)
	}
	hdrs := map[string][]string{}
	for k, v := range o.headers {
		hdrs[k] = v
	}
	if o.idempotencyKey != "" {
		hdrs[nats.MsgIdHdr] = []string{o.idempotencyKey}
	}
	p.mu.Lock()
	p.messages = append(p.messages, Message{Subject: subject, Data: data, Headers: hdrs})
	p.mu.Unlock()
	return nil
}

// Messages returns a snapshot of recorded messages.
func (p *NoopPublisher) Messages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Message, len(p.messages))
	copy(out, p.messages)
	return out
}

// Count returns the number of recorded messages.
func (p *NoopPublisher) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.messages)
}

// Close on NoopPublisher is a no-op.
func (p *NoopPublisher) Close() error { return nil }

// NewFromEnv returns a Publisher selected by env: a JetStream publisher when
// NATS_URL is set and reachable, otherwise NoopPublisher. Errors from a set
// NATS_URL are returned (do not silently fallback) so misconfiguration is loud.
func NewFromEnv(natsURL string) (Publisher, error) {
	if natsURL == "" {
		return NewNoop(), nil
	}
	return New(natsURL)
}
