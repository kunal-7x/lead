package callintel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	consumer "github.com/lead/services/call-intel/internal/consumer"
)

// roundTripFunc allows injecting an HTTP mock.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(b)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestSiteVisitConsumerCallsService(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/site-visits" && r.Method == http.MethodPost {
			called = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "sv-1"})
		}
	}))
	defer srv.Close()

	cfg := consumer.Config{
		SiteVisitURL: srv.URL,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := consumer.New(cfg, nil, nil, log)

	// Call the exported test helper (only compiled in tests).
	if err := r.TestCreateSiteVisit(context.Background(), consumer.SiteVisitRequestedPayload{
		TenantID:  "t1",
		LeadID:    "l1",
		ProjectID: "p1",
	}); err != nil {
		t.Fatalf("createSiteVisit: %v", err)
	}
	if !called {
		t.Error("expected site-visit service to be called")
	}
}

func TestCallbackEnqueueResilience(t *testing.T) {
	// 404 from scheduler should NOT return an error (soft fallback).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := consumer.Config{SchedulerURL: srv.URL}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := consumer.New(cfg, nil, nil, log)

	if err := r.TestEnqueueCallback(context.Background(), consumer.CallbackRequestedPayload{
		TenantID: "t1", LeadID: "l1", CampaignID: "c1", DelayMin: 5,
	}); err != nil {
		t.Fatalf("expected nil on 404 fallback, got: %v", err)
	}
	_ = time.Now() // keep import used
}
