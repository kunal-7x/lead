package dispatcher

// OutcomeConsumer subscribes to CAPSY_CALL (call.completed + call.failed)
// and drives the retry/disposition state machine for the dial queue.

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/lead/libs/go/events"
)

// CallOutcomeSubscriber is implemented by *events.JetStream; a no-op is used
// when NATS is unavailable.
type CallOutcomeSubscriber interface {
	// SubscribeCallOutcomes registers a callback for call.completed and call.failed.
	// Blocks until ctx is done.
	SubscribeCallOutcomes(ctx context.Context, fn func(eventID, providerCallID, status string)) error
}

// NATSCallOutcomeSubscriber wraps events.JetStream for call outcome events.
type NATSCallOutcomeSubscriber struct {
	js *events.JetStream
}

// NewNATSCallOutcomeSubscriber creates a subscriber for call outcome events.
func NewNATSCallOutcomeSubscriber(js *events.JetStream) *NATSCallOutcomeSubscriber {
	return &NATSCallOutcomeSubscriber{js: js}
}

// SubscribeCallOutcomes subscribes to call.completed and call.failed on CAPSY_CALL.
// Uses a filter on "call.completed" but the dispatcher processes both subjects —
// call.failed routes to the same queue via a wildcard filter consumer.
func (n *NATSCallOutcomeSubscriber) SubscribeCallOutcomes(ctx context.Context, fn func(eventID, providerCallID, status string)) error {
	// We use a single consumer with call.> filter covering both subjects.
	return n.js.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CALL",
		Durable:       "scheduler-outcome-consumer",
		FilterSubject: "call.>",
	}, func(ctx context.Context, subject string, data []byte, _ map[string][]string) error {
		// Only handle completed and failed lifecycle events.
		if subject != events.SubjectCallCompleted && subject != "call.failed" {
			return nil // ack and skip other subjects (ringing, answered, etc.)
		}
		var payload struct {
			EventID        string `json:"event_id"`
			ProviderCallID string `json:"provider_call_id"`
			Status         string `json:"status"`
		}
		if err := json.Unmarshal(data, &payload); err != nil || payload.ProviderCallID == "" {
			return nil // ack and discard malformed
		}
		fn(payload.EventID, payload.ProviderCallID, payload.Status)
		return nil
	})
}

// NoopCallOutcomeSubscriber silently does nothing. Used when NATS is unavailable.
type NoopCallOutcomeSubscriber struct{}

func (n *NoopCallOutcomeSubscriber) SubscribeCallOutcomes(_ context.Context, _ func(string, string, string)) error {
	return nil
}

// ----- Dispatcher: outcome subscription wiring ------------------------------

// SubscribeOutcomes registers the call-outcome consumer. Call once in a goroutine.
func (d *Dispatcher) SubscribeOutcomes(ctx context.Context, sub CallOutcomeSubscriber) {
	if err := sub.SubscribeCallOutcomes(ctx, func(eventID, providerCallID, status string) {
		if err := d.onCallOutcome(ctx, eventID, providerCallID, status); err != nil {
			log.Printf("dispatcher: outcome event %s error: %v", eventID, err)
		}
	}); err != nil {
		log.Printf("dispatcher: call-outcome subscribe error: %v", err)
	}
}

// onCallOutcome processes a single call outcome event idempotently.
func (d *Dispatcher) onCallOutcome(ctx context.Context, eventID, providerCallID, status string) error {
	// Fast idempotency check: if any row has already recorded this event_id, skip.
	alreadyProcessed, err := d.queue.FindByLastOutcomeEventID(ctx, eventID)
	if err != nil {
		return err
	}
	if alreadyProcessed != nil {
		log.Printf("dispatcher: outcome %s already processed (idempotent skip)", eventID)
		return nil
	}

	row, err := d.queue.FindByProviderCallID(ctx, providerCallID)
	if err != nil {
		return err
	}
	if row == nil {
		// Unknown call — may already be finalised or from a different service.
		log.Printf("dispatcher: outcome %s: provider_call_id %s not found in queue", eventID, providerCallID)
		return nil
	}

	p := extractRetryPolicy(row.CampaignCtx)
	delayMin := retryDelayMinutes(status, p)

	var updateErr error
	if status == "completed" {
		updateErr = d.queue.UpdateRow(ctx, row.ID, func(r *DialRow) {
			r.Status = "done"
			r.Disposition = "answered"
			r.ProviderCallID = providerCallID
			setLastOutcomeEventID(r, eventID)
		})
		log.Printf("dispatcher: row %s completed → done/answered", row.ID)
		return updateErr
	}

	// Retry path.
	newAttempts := row.Attempts + 1
	if newAttempts >= p.RetryMax {
		updateErr = d.queue.UpdateRow(ctx, row.ID, func(r *DialRow) {
			r.Attempts = newAttempts
			r.Status = "failed"
			r.Disposition = "exhausted"
			r.ProviderCallID = ""
			setLastOutcomeEventID(r, eventID)
		})
		log.Printf("dispatcher: row %s exhausted after %d attempts (status=%s)", row.ID, newAttempts, status)
		return updateErr
	}

	// Schedule retry.
	nextAt := time.Now().Add(time.Duration(delayMin) * time.Minute)
	updateErr = d.queue.UpdateRow(ctx, row.ID, func(r *DialRow) {
		r.Attempts = newAttempts
		r.Status = "pending"
		r.Disposition = status // transient disposition tracks last outcome
		r.ProviderCallID = ""
		r.NextAttemptAt = nextAt
		setLastOutcomeEventID(r, eventID)
	})
	log.Printf("dispatcher: row %s scheduled retry #%d at %v (status=%s, delay=%dm)", row.ID, newAttempts, nextAt.Format(time.RFC3339), status, delayMin)
	return updateErr
}

// setLastOutcomeEventID stores the event_id in the row's CampaignCtx to
// enable idempotent redelivery detection.
func setLastOutcomeEventID(r *DialRow, eventID string) {
	if r.CampaignCtx == nil {
		r.CampaignCtx = make(map[string]any)
	}
	r.CampaignCtx["_last_outcome_event_id"] = eventID
}
