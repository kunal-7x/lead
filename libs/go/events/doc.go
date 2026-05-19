// Package events provides a NATS JetStream publisher/subscriber used by all
// Capsy services. A NoopPublisher fallback is provided so services can run
// in tests and demos without a real NATS server.
package events
