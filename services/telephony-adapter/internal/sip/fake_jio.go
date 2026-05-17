// Package sip provides SIP trunk abstractions and test fakes.
package sip

import (
	"fmt"
	"sync"
)

// Trunk represents a SIP provider configuration.
type Trunk struct {
	Name    string
	Host    string
	Port    int
	Enabled bool
}

// FakeJioTrunk simulates the Jio SIP gateway for tests.
// Real implementation: IP-based registration, no username/password, port 5060, G.711.
// To integrate with real Jio: configure infra/freeswitch/conf/sip_profiles/external.xml
// with the actual Jio IP range and enable via ESL.
type FakeJioTrunk struct {
	mu        sync.Mutex
	registered bool
	invites    []string // synthetic INVITEs generated
}

func NewFakeJioTrunk() *FakeJioTrunk {
	return &FakeJioTrunk{}
}

func (f *FakeJioTrunk) Register() error {
	f.mu.Lock(); defer f.mu.Unlock()
	// Real Jio: IP-based, no auth challenge needed from Jio IP range.
	f.registered = true
	return nil
}

func (f *FakeJioTrunk) IsRegistered() bool {
	f.mu.Lock(); defer f.mu.Unlock()
	return f.registered
}

// GenerateInvite creates a synthetic inbound INVITE for testing.
func (f *FakeJioTrunk) GenerateInvite(from, to string) string {
	f.mu.Lock(); defer f.mu.Unlock()
	invite := fmt.Sprintf("INVITE sip:%s@fake-jio From: <sip:%s@jio.in>", to, from)
	f.invites = append(f.invites, invite)
	return invite
}

func (f *FakeJioTrunk) Invites() []string {
	f.mu.Lock(); defer f.mu.Unlock()
	out := make([]string, len(f.invites))
	copy(out, f.invites)
	return out
}

func (f *FakeJioTrunk) Config() Trunk {
	return Trunk{
		Name:    "jio",
		Host:    "jio-sip.example.com",
		Port:    5060,
		Enabled: f.registered,
	}
}
