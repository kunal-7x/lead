package esl

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// FakeClient simulates FreeSWITCH ESL responses for tests.
// When real FreeSWITCH is available, replace with TCPClient via Dial().
type FakeClient struct {
	mu       sync.Mutex
	healthy  bool
	calls    map[string]bool // uuid → active
	hangups  map[string]bool // uuid → hung up
	lastCmd  Command
}

func NewFakeClient() *FakeClient {
	return &FakeClient{
		healthy: true,
		calls:   make(map[string]bool),
		hangups: make(map[string]bool),
	}
}

func (f *FakeClient) SetHealthy(v bool) {
	f.mu.Lock(); defer f.mu.Unlock()
	f.healthy = v
}

func (f *FakeClient) LastCommand() Command {
	f.mu.Lock(); defer f.mu.Unlock()
	return f.lastCmd
}

func (f *FakeClient) SendCommand(_ context.Context, cmd Command) (*Response, error) {
	f.mu.Lock(); defer f.mu.Unlock()
	f.lastCmd = cmd

	s := string(cmd)

	switch {
	case strings.HasPrefix(s, "status"):
		if f.healthy {
			return &Response{Body: "UP 0 years, 0 days, 0 hours, 0 minutes, 0 seconds, 0 milliseconds, 0 microseconds"}, nil
		}
		return &Response{Body: "DOWN"}, nil

	case strings.HasPrefix(s, "originate"):
		// originate {origination_uuid=<uuid>}sofia/... → +OK <uuid>
		uuid := extractUUID(s)
		if uuid == "" {
			uuid = fmt.Sprintf("fake-uuid-%d", len(f.calls)+1)
		}
		f.calls[uuid] = true
		return &Response{Body: "+OK " + uuid}, nil

	case strings.HasPrefix(s, "uuid_kill"):
		parts := strings.Fields(s)
		if len(parts) >= 2 {
			uuid := parts[1]
			f.hangups[uuid] = true
			delete(f.calls, uuid)
		}
		return &Response{Body: "+OK"}, nil

	case strings.HasPrefix(s, "uuid_transfer"):
		return &Response{Body: "+OK"}, nil

	default:
		return &Response{Body: "+OK"}, nil
	}
}

func (f *FakeClient) Healthy(_ context.Context) bool {
	f.mu.Lock(); defer f.mu.Unlock()
	return f.healthy
}

func (f *FakeClient) Close() error { return nil }

func (f *FakeClient) WasHungUp(uuid string) bool {
	f.mu.Lock(); defer f.mu.Unlock()
	return f.hangups[uuid]
}

func extractUUID(cmd string) string {
	const marker = "origination_uuid="
	idx := strings.Index(cmd, marker)
	if idx < 0 {
		return ""
	}
	rest := cmd[idx+len(marker):]
	end := strings.IndexAny(rest, "} ")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
