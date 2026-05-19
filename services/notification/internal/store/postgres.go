package store

import (
	"context"
	"slices"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/notification/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_notification")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) RecordNotification(ctx context.Context, notification model.Notification) (model.Notification, error) {
	if notification.ID == "" {
		notification.ID = pgkv.NewID("notification")
	}
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = time.Now().UTC()
	}
	return notification, p.kv.Put(ctx, "notifications", notification.ID, notification)
}

func (p *PostgresStore) ListNotifications(ctx context.Context, tenantID string) ([]model.Notification, error) {
	items, err := pgkv.List[model.Notification](ctx, p.kv, "notifications")
	if err != nil {
		return nil, err
	}
	out := make([]model.Notification, 0, len(items))
	for _, item := range items {
		if tenantID == "" || item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *PostgresStore) SaveSubscription(ctx context.Context, sub model.Subscription) (model.Subscription, error) {
	if sub.ID == "" {
		sub.ID = pgkv.NewID("subscription")
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	return sub, p.kv.Put(ctx, "subscriptions", sub.ID, sub)
}

func (p *PostgresStore) ListSubscriptions(ctx context.Context, tenantID, eventType string) ([]model.Subscription, error) {
	items, err := pgkv.List[model.Subscription](ctx, p.kv, "subscriptions")
	if err != nil {
		return nil, err
	}
	out := make([]model.Subscription, 0, len(items))
	for _, item := range items {
		if tenantID != "" && item.TenantID != tenantID {
			continue
		}
		if eventType != "" && !slices.Contains(item.EventTypes, eventType) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (p *PostgresStore) EnqueueRetry(ctx context.Context, task model.RetryTask) (model.RetryTask, error) {
	if task.ID == "" {
		task.ID = pgkv.NewID("retry")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	return task, p.kv.Put(ctx, "retries", task.ID, task)
}

func (p *PostgresStore) ListRetries(ctx context.Context, tenantID string) ([]model.RetryTask, error) {
	items, err := pgkv.List[model.RetryTask](ctx, p.kv, "retries")
	if err != nil {
		return nil, err
	}
	out := make([]model.RetryTask, 0, len(items))
	for _, item := range items {
		if tenantID == "" || item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}
