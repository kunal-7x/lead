package dispatcher_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/dispatcher"
)

// ----- fake Redis writer for test -------------------------------------------

type fakeRedis struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newFakeRedis() *fakeRedis { return &fakeRedis{data: make(map[string][]byte)} }

func (f *fakeRedis) SetEX(_ context.Context, key string, value []byte, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	f.data[key] = cp
	return nil
}

func (f *fakeRedis) get(key string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.data[key]
	if !ok {
		return nil, false
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m, true
}

func (f *fakeRedis) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.data))
	for k := range f.data {
		out = append(out, k)
	}
	return out
}

// ----- fake Subscriber for test ---------------------------------------------

type fakeSubscriber struct {
	mu       sync.Mutex
	handlers []func(string)
}

func (s *fakeSubscriber) SubscribeCampaignLaunched(_ context.Context, fn func(string)) error {
	s.mu.Lock()
	s.handlers = append(s.handlers, fn)
	s.mu.Unlock()
	return nil
}

func (s *fakeSubscriber) fire(campaignID string) {
	s.mu.Lock()
	hs := make([]func(string), len(s.handlers))
	copy(hs, s.handlers)
	s.mu.Unlock()
	for _, h := range hs {
		h(campaignID)
	}
}

// ----- mock telephony server with concurrency cap ---------------------------

type mockTelephony struct {
	mu         sync.Mutex
	inFlight   int
	cap        int
	totalCalls int64
	maxSeen    int
	calls      []string // provider_call_ids issued

	// hangupCh: tests send a provider_call_id here to "free" a slot
	hangupCh chan string
}

func newMockTelephony(cap int) *mockTelephony {
	return &mockTelephony{cap: cap, hangupCh: make(chan string, 32)}
}

func (m *mockTelephony) hangup(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inFlight--
	if m.inFlight < 0 {
		m.inFlight = 0
	}
}

func (m *mockTelephony) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/calls" {
			http.Error(w, "not found", 404)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		m.mu.Lock()
		if m.inFlight >= m.cap {
			m.mu.Unlock()
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		m.inFlight++
		if m.inFlight > m.maxSeen {
			m.maxSeen = m.inFlight
		}
		n := atomic.AddInt64(&m.totalCalls, 1)
		pcid := fmt.Sprintf("provider-call-%d", n)
		m.calls = append(m.calls, pcid)
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":       req["session_id"],
			"provider":         "mock",
			"provider_call_id": pcid,
			"status":           "queued",
		})
	})
}

// ----- mock campaign server -------------------------------------------------

func mockCampaignServer(campaignID, tenantID, projectID string, leadIDs []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/campaigns/"+campaignID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            campaignID,
				"tenant_id":     tenantID,
				"project_id":    projectID,
				"kb_version_id": "kb-v1",
				"context": map[string]any{
					"product_description": "Test Product",
					"offer":               "10% off",
					"language":            "en-IN",
					"goal":                "qualify",
				},
			})
		case r.URL.Path == "/v1/campaigns/"+campaignID+"/leads":
			type lead struct {
				LeadID string `json:"lead_id"`
			}
			leads := make([]lead, len(leadIDs))
			for i, id := range leadIDs {
				leads[i] = lead{LeadID: id}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"leads": leads})
		case r.URL.Path == "/v1/campaigns/"+campaignID+"/limits":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"max_call_seconds": 300,
				"concurrent_cap":   3,
			})
		default:
			http.Error(w, "not found", 404)
		}
	}))
}

// ----- mock lead-import server ----------------------------------------------

func mockLeadImportServer(tenantID string, leadIDs []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract lead ID from /v1/leads/{id}
		parts := splitPath(r.URL.Path)
		if len(parts) != 3 || parts[0] != "v1" || parts[1] != "leads" {
			http.Error(w, "not found", 404)
			return
		}
		leadID := parts[2]
		// Check tenant header
		if r.Header.Get("X-Tenant-ID") != tenantID {
			http.Error(w, "forbidden", 403)
			return
		}
		// Return a phone for any recognized lead
		for _, id := range leadIDs {
			if id == leadID {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"lead": map[string]any{
						"id":        leadID,
						"tenant_id": tenantID,
						"phone_e164": fmt.Sprintf("+9198765%05s", leadID[len(leadID)-5:]),
					},
				})
				return
			}
		}
		http.Error(w, "not found", 404)
	}))
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i, c := range path {
		if c == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		parts = append(parts, path[start:])
	}
	return parts
}

// ----- main test ------------------------------------------------------------

// TestDispatcher_ConcurrencyCapAndAllLeadsPlaced asserts:
// (a) telephony never sees >3 concurrent calls,
// (b) all 7 leads are eventually placed,
// (c) a call:session:{id} Redis key was written for each placed call containing campaign_context.
func TestDispatcher_ConcurrencyCapAndAllLeadsPlaced(t *testing.T) {
	const (
		campaignID = "campaign-test-001"
		tenantID   = "tenant-test"
		projectID  = "proj-test"
		concCap    = 3
		nLeads     = 7
	)

	leadIDs := make([]string, nLeads)
	for i := range leadIDs {
		leadIDs[i] = fmt.Sprintf("lead-%05d", i+1)
	}

	tel := newMockTelephony(concCap)
	telSrv := httptest.NewServer(tel.handler())
	defer telSrv.Close()

	campSrv := mockCampaignServer(campaignID, tenantID, projectID, leadIDs)
	defer campSrv.Close()

	leadSrv := mockLeadImportServer(tenantID, leadIDs)
	defer leadSrv.Close()

	rdb := newFakeRedis()
	sub := &fakeSubscriber{}
	q := dispatcher.NewFakeDialQueueStore()

	d := dispatcher.New(sub, rdb, nil, q, dispatcher.Config{
		CampaignURL:          campSrv.URL,
		LeadImportURL:        leadSrv.URL,
		TelephonyURL:         telSrv.URL,
		FromNumber:           "+911400000000",
		PublicWebhookBaseURL: "https://test.example.com",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start subscriber (will register the handler).
	go d.Subscribe(ctx)
	time.Sleep(10 * time.Millisecond)

	// Fire the launch event.
	sub.fire(campaignID)

	// Give seeding time to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(q.AllRows()) == nLeads {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(q.AllRows()); got != nLeads {
		t.Fatalf("expected %d seeded rows, got %d", nLeads, got)
	}

	// Run dispatcher ticks while freeing telephony slots.
	// We run one tick, free slots, repeat until all placed.
	placed := 0
	for round := 0; round < 20 && placed < nLeads; round++ {
		// Run one tick.
		d.RunOneTick(ctx)

		// Count newly in_flight calls since last round and free slots.
		tel.mu.Lock()
		newCalls := tel.calls[placed:]
		tel.mu.Unlock()

		for _, pcid := range newCalls {
			tel.hangup(pcid)
			placed++
		}
		time.Sleep(5 * time.Millisecond)
	}

	// (a) Max concurrent never exceeded cap.
	tel.mu.Lock()
	maxSeen := tel.maxSeen
	totalCalls := tel.totalCalls
	tel.mu.Unlock()

	if maxSeen > concCap {
		t.Errorf("max concurrent calls %d exceeded cap %d", maxSeen, concCap)
	}

	// (b) All 7 leads placed.
	if int(totalCalls) != nLeads {
		t.Errorf("expected %d calls placed, got %d", nLeads, totalCalls)
	}

	// (c) Redis session keys written for each placed call.
	keys := rdb.keys()
	if len(keys) != nLeads {
		t.Errorf("expected %d redis keys, got %d: %v", nLeads, len(keys), keys)
	}
	for _, k := range keys {
		m, ok := rdb.get(k)
		if !ok {
			t.Errorf("key %s missing in redis", k)
			continue
		}
		if m["campaign_context"] == nil {
			t.Errorf("key %s: campaign_context missing, got %v", k, m)
		}
		if m["tenant_id"] != tenantID {
			t.Errorf("key %s: expected tenant_id %s, got %v", k, tenantID, m["tenant_id"])
		}
		if m["campaign_id"] != campaignID {
			t.Errorf("key %s: expected campaign_id %s, got %v", k, campaignID, m["campaign_id"])
		}
	}
}
