package recording

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lead/libs/go/pgjson"
)

type PostgresRecordingStore struct {
	db *pgxpool.Pool
}

type uploadRecord struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	SessionID string    `json:"session_id"`
	SpacesKey string    `json:"spaces_key"`
	Status    string    `json:"status"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewPostgresRecordingStore(dsn string) (*PostgresRecordingStore, error) {
	db, err := pgjson.OpenPool(context.Background(), dsn, "freeswitch_bridge")
	if err != nil {
		return nil, err
	}
	return &PostgresRecordingStore{db: db}, nil
}

func (s *PostgresRecordingStore) Close() {
	s.db.Close()
}

func (s *PostgresRecordingStore) SaveUpload(ctx context.Context, tenantID, sessionID, spacesKey string, sizeBytes int64) error {
	now := time.Now().UTC()
	record := uploadRecord{
		ID:        tenantID + ":" + sessionID,
		TenantID:  tenantID,
		SessionID: sessionID,
		SpacesKey: spacesKey,
		Status:    "uploaded",
		SizeBytes: sizeBytes,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := pgjson.Put(ctx, s.db, "upload", record.ID, tenantID, record); err != nil {
		return err
	}
	// Also reflect spaces_key + size into the queue row so dashboards
	// can read both signals without a join.
	_, _ = s.db.Exec(ctx, `
		UPDATE recording_upload_queue
		SET spaces_key = $1, size_bytes = $2, status = 'uploaded', updated_at = now()
		WHERE tenant_id = $3 AND session_id = $4
	`, spacesKey, sizeBytes, tenantID, sessionID)
	return nil
}

// SaveArchive records the B2 archive key for a given recording.
func (s *PostgresRecordingStore) SaveArchive(ctx context.Context, tenantID, sessionID, b2Key string) error {
	now := time.Now().UTC()
	type archiveRecord struct {
		ID         string    `json:"id"`
		TenantID   string    `json:"tenant_id"`
		SessionID  string    `json:"session_id"`
		B2Key      string    `json:"b2_key"`
		ArchivedAt time.Time `json:"archived_at"`
	}
	rec := archiveRecord{
		ID:         tenantID + ":" + sessionID,
		TenantID:   tenantID,
		SessionID:  sessionID,
		B2Key:      b2Key,
		ArchivedAt: now,
	}
	if err := pgjson.Put(ctx, s.db, "archive", rec.ID, tenantID, rec); err != nil {
		return err
	}
	_, _ = s.db.Exec(ctx, `
		UPDATE recording_upload_queue
		SET b2_key = $1, archived_at = now(), updated_at = now()
		WHERE tenant_id = $2 AND session_id = $3
	`, b2Key, tenantID, sessionID)
	return nil
}
