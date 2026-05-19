package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lead/libs/go/events"
)

type fakeStore struct {
	mu        sync.Mutex
	rows      []events.OutboxRow
	published map[string]bool
}

func (s *fakeStore) FetchUnpublished(_ context.Context, limit int) ([]events.OutboxRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []events.OutboxRow
	for _, r := range s.rows {
		if s.published[r.ID] {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) MarkPublished(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.published == nil {
		s.published = map[string]bool{}
	}
	s.published[id] = true
	return nil
}

func TestOutboxRelay_DrainsAndMarks(t *testing.T) {
	st := &fakeStore{
		rows: []events.OutboxRow{
			{ID: "1", Subject: "lead.scored", Payload: []byte("a")},
			{ID: "2", Subject: "lead.scored", Payload: []byte("b")},
		},
	}
	pub := events.NewNoop()
	r := &events.OutboxRelay{
		Store:     st,
		Publisher: pub,
		Interval:  10 * time.Millisecond,
		BatchSize: 10,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := r.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if pub.Count() != 2 {
		t.Fatalf("expected 2 publishes, got %d", pub.Count())
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.published["1"] || !st.published["2"] {
		t.Fatalf("rows not marked published: %v", st.published)
	}
}
