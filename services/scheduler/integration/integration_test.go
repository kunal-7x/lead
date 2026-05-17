//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/scheduler/internal/handler"
	"github.com/lead/services/scheduler/internal/model"
	"github.com/lead/services/scheduler/internal/picker"
	"github.com/lead/services/scheduler/internal/store"
)

func newTestServer() *httptest.Server {
	s := store.NewFake()
	p := picker.New(s)
	h := handler.New(p)
	return httptest.NewServer(h.Routes())
}

func TestIntegration_Healthz(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestIntegration_PickNext_EmptyCampaign(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	body, _ := json.Marshal(model.PickNextRequest{
		CampaignID: "camp-x",
		WorkerID:   "w1",
		BatchSize:  5,
	})
	resp, err := http.Post(srv.URL+"/v1/scheduler/pick-next", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result model.PickNextResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Leads) != 0 {
		t.Fatalf("expected 0 leads for empty campaign, got %d", len(result.Leads))
	}
}

func TestIntegration_QueueDepth_MissingTenantID(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/scheduler/queue-depth")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIntegration_QueueDepth(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s/v1/scheduler/queue-depth?tenant_id=t1", srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result model.QueueDepthResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.Depth != 0 {
		t.Fatalf("expected 0 depth, got %d", result.Depth)
	}
}
