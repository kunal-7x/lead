package store

import (
	"context"

	"github.com/lead/services/notification/internal/model"
)

type Store interface {
	RecordNotification(context.Context, model.Notification) (model.Notification, error)
	ListNotifications(context.Context, string) ([]model.Notification, error)
	SaveSubscription(context.Context, model.Subscription) (model.Subscription, error)
	ListSubscriptions(context.Context, string, string) ([]model.Subscription, error)
	EnqueueRetry(context.Context, model.RetryTask) (model.RetryTask, error)
	ListRetries(context.Context, string) ([]model.RetryTask, error)
}
