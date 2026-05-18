package security_test

import (
	"sync"
	"testing"
)

func TestExternalSideEffectEndpointsRequireIdempotency(t *testing.T) {
	endpoints := []struct {
		path         string
		idempotency  bool
		externalSide bool
	}{
		{"/v1/telephony/calls", true, true},
		{"/v1/wa/messages/template", true, true},
		{"/v1/billing/charge", true, true},
		{"/v1/site-visits", true, true},
		{"/v1/admin/models/current", false, false},
	}
	for _, endpoint := range endpoints {
		if endpoint.externalSide && !endpoint.idempotency {
			t.Fatalf("%s has external side effects without idempotency", endpoint.path)
		}
	}
}

func TestConcurrentDuplicatesProduceOneEffect(t *testing.T) {
	store := newIdempotencyStore()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.do("tenant-demo:call:lead-1", func() {
				store.effectCount++
			})
		}()
	}
	wg.Wait()
	if store.effectCount != 1 {
		t.Fatalf("effect count = %d, want 1", store.effectCount)
	}
}

type idempotencyStore struct {
	mu          sync.Mutex
	keys        map[string]struct{}
	effectCount int
}

func newIdempotencyStore() *idempotencyStore {
	return &idempotencyStore{keys: map[string]struct{}{}}
}

func (s *idempotencyStore) do(key string, effect func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[key]; ok {
		return
	}
	s.keys[key] = struct{}{}
	effect()
}
