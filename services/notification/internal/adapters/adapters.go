package adapters

import (
	"context"
	"errors"
	"sync"

	"github.com/lead/services/notification/internal/model"
)

type Message struct {
	TenantID  string
	Channel   model.Channel
	Recipient string
	Template  string
	Payload   map[string]any
}

type Adapter interface {
	Send(context.Context, Message) error
}

type Fake struct {
	mu      sync.Mutex
	Sent    []Message
	NextErr error
}

func NewFake() *Fake {
	return &Fake{}
}

func (f *Fake) Send(_ context.Context, msg Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextErr != nil {
		err := f.NextErr
		f.NextErr = nil
		return err
	}
	f.Sent = append(f.Sent, msg)
	return nil
}

func (f *Fake) Count(channel model.Channel) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, msg := range f.Sent {
		if msg.Channel == channel {
			count++
		}
	}
	return count
}

type Dashboard struct{ *Fake }
type WhatsApp struct{ *Fake }
type Email struct{ *Fake }
type Webhook struct{ *Fake }

func NewDashboard() *Dashboard { return &Dashboard{Fake: NewFake()} }
func NewWhatsApp() *WhatsApp   { return &WhatsApp{Fake: NewFake()} }
func NewEmail() *Email         { return &Email{Fake: NewFake()} }
func NewWebhook() *Webhook     { return &Webhook{Fake: NewFake()} }

var ErrNoAdapter = errors.New("notification adapter not configured")
