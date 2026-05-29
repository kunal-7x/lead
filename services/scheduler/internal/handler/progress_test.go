package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lead/services/scheduler/internal/dispatcher"
	"github.com/lead/services/scheduler/internal/handler"
	"github.com/lead/services/scheduler/internal/picker"
	"github.com/lead/services/scheduler/internal/store"
)

func TestCampaignProgress(t *testing.T) {
	const campaignID = "camp-1"

	// Seed fake queue with a known mix:
	//   2 pending (1 never attempted, 1 re-queued with attempts>0)
	//   1 in_flight
	//   3 done (2 answered, 1 with empty disposition)
	//   1 failed
	//   1 suppressed
	q := dispatcher.NewFakeDialQueueStore()
	ctx := context.Background()

	seed := []*dispatcher.DialRow{
		{CampaignID: campaignID, LeadID: "L1", Status: "pending", Attempts: 0, NextAttemptAt: time.Now().Add(-time.Minute)},
		{CampaignID: campaignID, LeadID: "L2", Status: "pending", Attempts: 1, NextAttemptAt: time.Now().Add(-time.Minute)},
		{CampaignID: campaignID, LeadID: "L3", Status: "in_flight", Attempts: 1},
		{CampaignID: campaignID, LeadID: "L4", Status: "done", Disposition: "answered", Attempts: 1},
		{CampaignID: campaignID, LeadID: "L5", Status: "done", Disposition: "answered", Attempts: 1},
		{CampaignID: campaignID, LeadID: "L6", Status: "done", Disposition: "", Attempts: 1},
		{CampaignID: campaignID, LeadID: "L7", Status: "failed", Attempts: 1},
		{CampaignID: campaignID, LeadID: "L8", Status: "suppressed", Disposition: "dnd", Attempts: 0},
	}
	if err := q.SeedRows(ctx, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Build handler with fake store wired in
	fakeStore := store.NewFake()
	p := picker.New(fakeStore)
	h := handler.NewWithTelephony(p, "http://unused", http.DefaultClient).WithDialQueue(q)

	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/campaigns/" + campaignID + "/progress")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	var got handler.CampaignProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	check := func(name string, gotVal, want int) {
		t.Helper()
		if gotVal != want {
			t.Errorf("%s: got %d, want %d", name, gotVal, want)
		}
	}

	if got.CampaignID != campaignID {
		t.Errorf("campaign_id: got %q, want %q", got.CampaignID, campaignID)
	}
	check("total", got.Total, 8)
	check("pending", got.Pending, 2)
	check("in_flight", got.InFlight, 1)
	check("done", got.Done, 3)
	check("failed", got.Failed, 1)
	check("suppressed", got.Suppressed, 1)
	check("placed", got.Placed, 5)       // in_flight(1) + done(3) + failed(1)
	check("connected", got.Connected, 2) // answered disposition
	check("no_answer_retry", got.NoAnswerRetry, 1) // pending with attempts>0
}
