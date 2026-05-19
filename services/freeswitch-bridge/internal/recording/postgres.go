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
	return pgjson.Put(ctx, s.db, "upload", record.ID, tenantID, record)
}
