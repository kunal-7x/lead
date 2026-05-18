package service

import (
	"context"
	"errors"
	"time"

	"github.com/lead/services/notification/internal/adapters"
	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/store"
)

type Service struct {
	store    store.Store
	adapters map[model.Channel]adapters.Adapter
	now      func() time.Time
}

func New(st store.Store, configured map[model.Channel]adapters.Adapter) *Service {
	if configured == nil {
		configured = map[model.Channel]adapters.Adapter{}
	}
	return &Service{
		store:    st,
		adapters: configured,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) Send(ctx context.Context, req model.SendRequest) (model.Notification, error) {
	if req.TenantID == "" || req.Channel == "" || req.Recipient == "" || req.Template == "" {
		return model.Notification{}, errors.New("tenant_id, channel, recipient and template are required")
	}
	adapter, ok := s.adapters[req.Channel]
	if !ok {
		notification, _ := s.record(ctx, req, model.StatusFailed, adapters.ErrNoAdapter.Error())
		_ = s.enqueueRetry(ctx, notification, adapters.ErrNoAdapter)
		return notification, adapters.ErrNoAdapter
	}
	err := adapter.Send(ctx, adapters.Message{
		TenantID:  req.TenantID,
		Channel:   req.Channel,
		Recipient: req.Recipient,
		Template:  req.Template,
		Payload:   req.Payload,
	})
	if err != nil {
		notification, recordErr := s.record(ctx, req, model.StatusFailed, err.Error())
		if recordErr != nil {
			return model.Notification{}, recordErr
		}
		_ = s.enqueueRetry(ctx, notification, err)
		return notification, err
	}
	return s.record(ctx, req, model.StatusSent, "")
}

func (s *Service) Subscribe(ctx context.Context, sub model.Subscription) (model.Subscription, error) {
	if sub.TenantID == "" || len(sub.EventTypes) == 0 || len(sub.Channels) == 0 || len(sub.Recipients) == 0 {
		return model.Subscription{}, errors.New("tenant_id, event_types, channels and recipients are required")
	}
	if sub.Template == "" {
		sub.Template = "handoff-alert"
	}
	return s.store.SaveSubscription(ctx, sub)
}

func (s *Service) FanOut(ctx context.Context, event model.Event) ([]model.Notification, error) {
	subs, err := s.store.ListSubscriptions(ctx, event.TenantID, event.Type)
	if err != nil {
		return nil, err
	}
	var sent []model.Notification
	for _, sub := range subs {
		for i, channel := range sub.Channels {
			recipient := sub.Recipients[0]
			if i < len(sub.Recipients) {
				recipient = sub.Recipients[i]
			}
			notification, err := s.Send(ctx, model.SendRequest{
				TenantID:  sub.TenantID,
				Channel:   channel,
				Recipient: recipient,
				Template:  sub.Template,
				Payload:   event.Payload,
			})
			sent = append(sent, notification)
			if err != nil {
				return sent, err
			}
		}
	}
	return sent, nil
}

func (s *Service) Store() store.Store {
	return s.store
}

func (s *Service) record(ctx context.Context, req model.SendRequest, status model.Status, errMsg string) (model.Notification, error) {
	return s.store.RecordNotification(ctx, model.Notification{
		TenantID:  req.TenantID,
		Channel:   req.Channel,
		Recipient: req.Recipient,
		Template:  req.Template,
		Payload:   req.Payload,
		Status:    status,
		Error:     errMsg,
		CreatedAt: s.now(),
	})
}

func (s *Service) enqueueRetry(ctx context.Context, notification model.Notification, sendErr error) error {
	_, err := s.store.EnqueueRetry(ctx, model.RetryTask{
		TenantID:       notification.TenantID,
		NotificationID: notification.ID,
		Channel:        notification.Channel,
		Recipient:      notification.Recipient,
		RunAfter:       s.now().Add(30 * time.Second),
		Attempts:       1,
		LastError:      sendErr.Error(),
		CreatedAt:      s.now(),
	})
	return err
}
