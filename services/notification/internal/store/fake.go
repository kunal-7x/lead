package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lead/services/notification/internal/model"
)

type Fake struct {
	mu            sync.Mutex
	seq           int
	notifications []*model.Notification
	subscriptions []*model.Subscription
	retries       []*model.RetryTask
}

func NewFake() *Fake {
	return &Fake{}
}

func (f *Fake) RecordNotification(_ context.Context, notification model.Notification) (model.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if notification.ID == "" {
		notification.ID = f.nextID("notification")
	}
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = time.Now().UTC()
	}
	cp := cloneNotification(notification)
	f.notifications = append(f.notifications, &cp)
	return notification, nil
}

func (f *Fake) ListNotifications(_ context.Context, tenantID string) ([]model.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Notification, 0, len(f.notifications))
	for _, notification := range f.notifications {
		if tenantID == "" || notification.TenantID == tenantID {
			out = append(out, cloneNotification(*notification))
		}
	}
	return out, nil
}

func (f *Fake) SaveSubscription(_ context.Context, sub model.Subscription) (model.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sub.ID == "" {
		sub.ID = f.nextID("subscription")
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	cp := cloneSubscription(sub)
	f.subscriptions = append(f.subscriptions, &cp)
	return sub, nil
}

func (f *Fake) ListSubscriptions(_ context.Context, tenantID, eventType string) ([]model.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Subscription, 0, len(f.subscriptions))
	for _, sub := range f.subscriptions {
		if tenantID != "" && sub.TenantID != tenantID {
			continue
		}
		if eventType != "" && !slices.Contains(sub.EventTypes, eventType) {
			continue
		}
		out = append(out, cloneSubscription(*sub))
	}
	return out, nil
}

func (f *Fake) EnqueueRetry(_ context.Context, task model.RetryTask) (model.RetryTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if task.ID == "" {
		task.ID = f.nextID("retry")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	cp := task
	f.retries = append(f.retries, &cp)
	return task, nil
}

func (f *Fake) ListRetries(_ context.Context, tenantID string) ([]model.RetryTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.RetryTask, 0, len(f.retries))
	for _, task := range f.retries {
		if tenantID == "" || task.TenantID == tenantID {
			out = append(out, *task)
		}
	}
	return out, nil
}

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%06d", prefix, f.seq)
}

func cloneNotification(n model.Notification) model.Notification {
	n.Payload = cloneMap(n.Payload)
	return n
}

func cloneSubscription(s model.Subscription) model.Subscription {
	s.EventTypes = append([]string(nil), s.EventTypes...)
	s.Channels = append([]model.Channel(nil), s.Channels...)
	s.Recipients = append([]string(nil), s.Recipients...)
	return s
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
