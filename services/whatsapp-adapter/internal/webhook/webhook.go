package webhook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

type EventType string

const (
	EventReply    EventType = "reply"
	EventOptOut   EventType = "opt_out"
	EventDelivery EventType = "delivery"
	EventRead     EventType = "read"
)

type NormalizedEvent struct {
	ID        string
	Type      EventType
	TenantID  string
	LeadID    string
	Phone     string
	Body      string
	MessageID string
	Status    string
	Timestamp time.Time
}

func Normalize(body []byte) ([]NormalizedEvent, error) {
	var payload struct {
		TenantID string `json:"tenant_id"`
		LeadID   string `json:"lead_id"`
		Events   []struct {
			ID        string    `json:"id"`
			Type      EventType `json:"type"`
			TenantID  string    `json:"tenant_id"`
			LeadID    string    `json:"lead_id"`
			Phone     string    `json:"phone"`
			Body      string    `json:"body"`
			MessageID string    `json:"message_id"`
			Status    string    `json:"status"`
			Timestamp string    `json:"timestamp"`
		} `json:"events"`
		Entry []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						ID        string `json:"id"`
						From      string `json:"from"`
						Type      string `json:"type"`
						Timestamp string `json:"timestamp"`
						Text      struct {
							Body string `json:"body"`
						} `json:"text"`
					} `json:"messages"`
					Statuses []struct {
						ID          string `json:"id"`
						Status      string `json:"status"`
						RecipientID string `json:"recipient_id"`
						Timestamp   string `json:"timestamp"`
					} `json:"statuses"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	var out []NormalizedEvent
	for _, event := range payload.Events {
		typ := event.Type
		if typ == "" && event.Status != "" {
			typ = EventDelivery
			if event.Status == "read" {
				typ = EventRead
			}
		}
		if typ == EventReply && IsOptOutBody(event.Body) {
			typ = EventOptOut
		}
		out = append(out, NormalizedEvent{
			ID:        event.ID,
			Type:      typ,
			TenantID:  first(event.TenantID, payload.TenantID),
			LeadID:    first(event.LeadID, payload.LeadID),
			Phone:     event.Phone,
			Body:      event.Body,
			MessageID: event.MessageID,
			Status:    event.Status,
			Timestamp: parseTimestamp(event.Timestamp),
		})
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				typ := EventReply
				if IsOptOutBody(msg.Text.Body) {
					typ = EventOptOut
				}
				out = append(out, NormalizedEvent{
					ID:        msg.ID,
					Type:      typ,
					TenantID:  payload.TenantID,
					LeadID:    payload.LeadID,
					Phone:     msg.From,
					Body:      msg.Text.Body,
					MessageID: msg.ID,
					Timestamp: parseTimestamp(msg.Timestamp),
				})
			}
			for _, status := range change.Value.Statuses {
				typ := EventDelivery
				if status.Status == "read" {
					typ = EventRead
				}
				out = append(out, NormalizedEvent{
					ID:        status.ID + ":" + status.Status,
					Type:      typ,
					TenantID:  payload.TenantID,
					LeadID:    payload.LeadID,
					Phone:     status.RecipientID,
					MessageID: status.ID,
					Status:    status.Status,
					Timestamp: parseTimestamp(status.Timestamp),
				})
			}
		}
	}

	if len(out) == 0 {
		sum := sha256.Sum256(body)
		out = append(out, NormalizedEvent{
			ID:        hex.EncodeToString(sum[:]),
			Type:      EventDelivery,
			TenantID:  payload.TenantID,
			LeadID:    payload.LeadID,
			Timestamp: time.Now().UTC(),
		})
	}
	for i := range out {
		if out[i].ID == "" {
			sum := sha256.Sum256(append(body, byte(i)))
			out[i].ID = hex.EncodeToString(sum[:])
		}
		if out[i].Timestamp.IsZero() {
			out[i].Timestamp = time.Now().UTC()
		}
	}
	return out, nil
}

func IsOptOutBody(body string) bool {
	normalized := strings.Trim(strings.ToLower(body), " \t\r\n.!?")
	switch normalized {
	case "stop", "unsubscribe", "opt out", "opt-out":
		return true
	default:
		return false
	}
}

func parseTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts.UTC()
	}
	return time.Time{}
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
