package store

import (
	"context"
	"time"

	"github.com/lead/libs/go/pgkv"
)

type PostgresStore struct {
	kv *pgkv.Store
}

type Instance struct {
	ID          string     `json:"id"`
	Host        string     `json:"host"`
	SIPPort     int        `json:"sip_port"`
	ESLPort     int        `json:"esl_port"`
	Healthy     bool       `json:"healthy"`
	LastChecked *time.Time `json:"last_checked,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type uploadRecord struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	SessionID string    `json:"session_id"`
	SpacesKey string    `json:"spaces_key"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type sessionRecord struct {
	CallUUID  string     `json:"call_uuid"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_freeswitch_bridge")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) SaveInstance(ctx context.Context, instance Instance) error {
	if instance.ID == "" {
		instance.ID = pgkv.NewID("fs")
	}
	if instance.CreatedAt.IsZero() {
		instance.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "freeswitch_instances", instance.ID, instance)
}

func (p *PostgresStore) ListInstances(ctx context.Context) ([]Instance, error) {
	return pgkv.List[Instance](ctx, p.kv, "freeswitch_instances")
}

func (p *PostgresStore) SaveUpload(ctx context.Context, tenantID, sessionID, spacesKey string, sizeBytes int64) error {
	record := uploadRecord{
		ID:        pgkv.NewID("upload"),
		TenantID:  tenantID,
		SessionID: sessionID,
		SpacesKey: spacesKey,
		SizeBytes: sizeBytes,
		CreatedAt: time.Now().UTC(),
	}
	return p.kv.Put(ctx, "recording_uploads", record.ID, record)
}

func (p *PostgresStore) SetSessionStarted(ctx context.Context, callUUID string) error {
	now := time.Now().UTC()
	record := sessionRecord{CallUUID: callUUID, StartedAt: &now, UpdatedAt: now}
	return p.kv.Put(ctx, "sessions", callUUID, record)
}

func (p *PostgresStore) SetSessionEnded(ctx context.Context, callUUID string) error {
	record, ok, err := pgkv.Get[sessionRecord](ctx, p.kv, "sessions", callUUID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if !ok {
		record = sessionRecord{CallUUID: callUUID}
	}
	record.EndedAt = &now
	record.UpdatedAt = now
	return p.kv.Put(ctx, "sessions", callUUID, record)
}
