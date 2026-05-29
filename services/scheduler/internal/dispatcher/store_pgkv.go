package dispatcher

// PgkvDialQueueStore implements DialQueueStore backed by pgkv.
// Collection/kind: "dial_queue". Key: row.ID.
// Idempotent seed: key = "{campaign_id}:{lead_id}" — first write wins.
//
// PgkvSuppressionStore implements SuppressionStore backed by pgkv.
// kind: "suppression", key: "{tenant_id}:{phone_e164}"

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lead/libs/go/pgkv"
)

// PgkvDialQueueStore is a durable DialQueueStore backed by a pgkv.Store.
type PgkvDialQueueStore struct {
	kv *pgkv.Store
}

// NewPgkvDialQueueStore connects to Postgres and returns a store.
// schema is passed as "svc_scheduler_dq" (its own namespace).
func NewPgkvDialQueueStore(ctx context.Context, dsn string) (*PgkvDialQueueStore, error) {
	kv, err := pgkv.New(ctx, dsn, "svc_scheduler_dq")
	if err != nil {
		return nil, fmt.Errorf("dial-queue store: %w", err)
	}
	return &PgkvDialQueueStore{kv: kv}, nil
}

// Close shuts down the underlying pool.
func (s *PgkvDialQueueStore) Close() { s.kv.Close() }

const dialQueueKind = "dial_queue"

// SeedRows inserts rows idempotently.  Key = "{campaign_id}:{lead_id}" so
// re-launching a campaign does not duplicate rows.
func (s *PgkvDialQueueStore) SeedRows(ctx context.Context, rows []*DialRow) error {
	for _, r := range rows {
		key := r.CampaignID + ":" + r.LeadID
		// Check if it already exists; skip if so.
		_, exists, err := pgkv.Get[DialRow](ctx, s.kv, dialQueueKind, key)
		if err != nil {
			return fmt.Errorf("SeedRows get: %w", err)
		}
		if exists {
			continue
		}
		cp := *r
		cp.ID = key // use composite key as canonical ID inside the store
		if err := s.kv.Put(ctx, dialQueueKind, key, cp); err != nil {
			return fmt.Errorf("SeedRows put: %w", err)
		}
	}
	return nil
}

// PendingRows returns all rows for a campaign that are pending and due.
func (s *PgkvDialQueueStore) PendingRows(ctx context.Context, campaignID string, now time.Time) ([]*DialRow, error) {
	all, err := pgkv.List[DialRow](ctx, s.kv, dialQueueKind)
	if err != nil {
		return nil, fmt.Errorf("PendingRows list: %w", err)
	}
	var out []*DialRow
	for i := range all {
		r := &all[i]
		if r.CampaignID != campaignID || r.Status != "pending" {
			continue
		}
		if r.NextAttemptAt.After(now) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// MarkInFlight sets a row to in_flight and records the provider call ID.
func (s *PgkvDialQueueStore) MarkInFlight(ctx context.Context, rowID, providerCallID string) error {
	return s.updateRow(ctx, rowID, func(r *DialRow) {
		r.Status = "in_flight"
		r.ProviderCallID = providerCallID
		r.Attempts++
	})
}

// MarkFailed sets a row to failed.
func (s *PgkvDialQueueStore) MarkFailed(ctx context.Context, rowID string) error {
	return s.updateRow(ctx, rowID, func(r *DialRow) {
		r.Status = "failed"
	})
}

// FindByProviderCallID scans in-flight rows and returns the matching row, or nil.
func (s *PgkvDialQueueStore) FindByProviderCallID(ctx context.Context, providerCallID string) (*DialRow, error) {
	all, err := pgkv.List[DialRow](ctx, s.kv, dialQueueKind)
	if err != nil {
		return nil, fmt.Errorf("FindByProviderCallID list: %w", err)
	}
	for i := range all {
		r := &all[i]
		if r.ProviderCallID == providerCallID {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

// FindByLastOutcomeEventID scans all rows for _last_outcome_event_id == eventID.
func (s *PgkvDialQueueStore) FindByLastOutcomeEventID(ctx context.Context, eventID string) (*DialRow, error) {
	all, err := pgkv.List[DialRow](ctx, s.kv, dialQueueKind)
	if err != nil {
		return nil, fmt.Errorf("FindByLastOutcomeEventID list: %w", err)
	}
	for i := range all {
		r := &all[i]
		if r.CampaignCtx != nil {
			if v, ok := r.CampaignCtx["_last_outcome_event_id"].(string); ok && v == eventID {
				cp := *r
				return &cp, nil
			}
		}
	}
	return nil, nil
}

// UpdateRow fetches, applies fn, and persists the row identified by rowID.
func (s *PgkvDialQueueStore) UpdateRow(ctx context.Context, rowID string, fn func(*DialRow)) error {
	return s.updateRow(ctx, rowID, fn)
}

// ActiveCampaignIDs returns distinct campaign IDs with pending or in_flight rows.
func (s *PgkvDialQueueStore) ActiveCampaignIDs(ctx context.Context) ([]string, error) {
	all, err := pgkv.List[DialRow](ctx, s.kv, dialQueueKind)
	if err != nil {
		return nil, fmt.Errorf("ActiveCampaignIDs list: %w", err)
	}
	seen := make(map[string]struct{})
	for _, r := range all {
		if r.Status == "pending" || r.Status == "in_flight" {
			seen[r.CampaignID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

// updateRow fetches, mutates, and re-stores a row by rowID.
func (s *PgkvDialQueueStore) updateRow(ctx context.Context, rowID string, fn func(*DialRow)) error {
	r, exists, err := pgkv.Get[DialRow](ctx, s.kv, dialQueueKind, rowID)
	if err != nil {
		return fmt.Errorf("updateRow get %s: %w", rowID, err)
	}
	if !exists {
		log.Printf("dial-queue store: row %s not found for update", rowID)
		return fmt.Errorf("row %s not found", rowID)
	}
	fn(&r)
	if err := s.kv.Put(ctx, dialQueueKind, rowID, r); err != nil {
		return fmt.Errorf("updateRow put %s: %w", rowID, err)
	}
	return nil
}

// ----- PgkvSuppressionStore -------------------------------------------------

const suppressionKind = "suppression"

// suppressionEntry is the value stored for a suppressed phone.
type suppressionEntry struct {
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// PgkvSuppressionStore implements SuppressionStore backed by pgkv.
type PgkvSuppressionStore struct {
	kv *pgkv.Store
}

// NewPgkvSuppressionStore creates a suppression store using the same DSN.
func NewPgkvSuppressionStore(ctx context.Context, dsn string) (*PgkvSuppressionStore, error) {
	kv, err := pgkv.New(ctx, dsn, "svc_scheduler_suppression")
	if err != nil {
		return nil, fmt.Errorf("suppression store: %w", err)
	}
	return &PgkvSuppressionStore{kv: kv}, nil
}

// Close shuts down the underlying pool.
func (s *PgkvSuppressionStore) Close() { s.kv.Close() }

// IsSuppressed returns true when the key tenant:phone exists in the store.
func (s *PgkvSuppressionStore) IsSuppressed(ctx context.Context, tenantID, phone string) bool {
	key := tenantID + ":" + phone
	_, exists, err := pgkv.Get[suppressionEntry](ctx, s.kv, suppressionKind, key)
	if err != nil {
		log.Printf("suppression store: IsSuppressed get %s: %v", key, err)
		return false // fail-open: don't block calls on DB errors
	}
	return exists
}

// Suppress records a phone as suppressed with the given reason.
func (s *PgkvSuppressionStore) Suppress(ctx context.Context, tenantID, phone, reason string) error {
	key := tenantID + ":" + phone
	entry := suppressionEntry{Reason: reason, CreatedAt: time.Now()}
	if err := s.kv.Put(ctx, suppressionKind, key, entry); err != nil {
		return fmt.Errorf("suppression store: Suppress put %s: %w", key, err)
	}
	return nil
}
