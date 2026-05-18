package store

import (
	"context"
	"strings"
	"sync"
)

type Fake struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewFake() *Fake {
	return &Fake{data: map[string]string{}}
}

func (f *Fake) Get(_ context.Context, key string) (string, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	value, ok := f.data[key]
	return value, ok, nil
}

func (f *Fake) Set(_ context.Context, key string, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *Fake) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

func (f *Fake) Keys(_ context.Context, prefix string) (map[string]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := map[string]string{}
	for key, value := range f.data {
		if strings.HasPrefix(key, prefix) {
			out[key] = value
		}
	}
	return out, nil
}
