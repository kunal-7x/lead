// Package store persists post-call intelligence records.
package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/lead/libs/go/pgkv"
)

// Transcript is the persisted record for a completed call.
type Transcript struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id"`
	TenantID   string     `json:"tenant_id"`
	CampaignID string     `json:"campaign_id,omitempty"`
	LeadID     string     `json:"lead_id"`
	ProjectID  string     `json:"project_id,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	Status     string     `json:"status,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	LeadScore  int        `json:"lead_score,omitempty"`
	DurationS  float64    `json:"duration_s,omitempty"`
	TurnCount  int        `json:"turn_count,omitempty"`
	Turns      []Turn     `json:"turns,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Turn is a single dialogue turn in a call transcript.
type Turn struct {
	Index      int     `json:"index"`
	Role       string  `json:"role"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence,omitempty"`
}

// Store persists transcripts.
type Store struct {
	kv *pgkv.Store
}

// New creates a Store backed by Postgres via pgkv.
func New(ctx context.Context, dsn string) (*Store, error) {
	kv, err := pgkv.New(ctx, dsn, "svc_call_intel")
	if err != nil {
		return nil, err
	}
	return &Store{kv: kv}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.kv.Close() }

// SaveTranscript persists a transcript record (upsert by session_id).
func (s *Store) SaveTranscript(ctx context.Context, t Transcript) (Transcript, error) {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	if err := s.kv.Put(ctx, "transcripts", t.ID, t); err != nil {
		return Transcript{}, err
	}
	// secondary index by session_id
	_ = s.kv.Put(ctx, "transcripts_by_session", t.SessionID, t)
	return t, nil
}

// GetBySessionID looks up a transcript by session_id. Returns (nil, nil) if
// not found.
func (s *Store) GetBySessionID(ctx context.Context, sessionID string) (*Transcript, error) {
	t, ok, err := pgkv.Get[Transcript](ctx, s.kv, "transcripts_by_session", sessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &t, nil
}
