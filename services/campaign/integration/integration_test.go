//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/campaign/internal/handler"
	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/store"
)

func newTestServer() (*httptest.Server, *store.Fake) {
	s := store.NewFake()
	h := handler.New(s)
	return httptest.NewServer(h.Router()), s
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func getJSON(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()

	resp := getJSON(t, srv, "/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateAndGetCampaign(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()

	var created model.Campaign
	resp := postJSON(t, srv, "/v1/campaigns/", map[string]string{
		"name":          "Integration Test Campaign",
		"project_id":    "p1",
		"tenant_id":     "t1",
		"kb_version_id": "kb1",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	decode(t, resp, &created)
	if created.ID == "" {
		t.Fatal("expected campaign ID to be set")
	}
	if created.Status != model.StatusDraft {
		t.Fatalf("expected draft status, got %s", created.Status)
	}

	// GET the campaign back.
	resp2 := getJSON(t, srv, "/v1/campaigns/"+created.ID)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	var fetched model.Campaign
	decode(t, resp2, &fetched)
	if fetched.ID != created.ID {
		t.Fatalf("expected id %s, got %s", created.ID, fetched.ID)
	}
}

func TestListCampaigns(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()

	postJSON(t, srv, "/v1/campaigns/", map[string]string{
		"name":          "C1",
		"project_id":    "p1",
		"tenant_id":     "t1",
		"kb_version_id": "kb1",
	})
	postJSON(t, srv, "/v1/campaigns/", map[string]string{
		"name":          "C2",
		"project_id":    "p2",
		"tenant_id":     "t1",
		"kb_version_id": "kb2",
	})

	resp := getJSON(t, srv, "/v1/campaigns/?tenant_id=t1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var campaigns []*model.Campaign
	decode(t, resp, &campaigns)
	if len(campaigns) < 2 {
		t.Fatalf("expected at least 2 campaigns, got %d", len(campaigns))
	}
}

func TestAttachLeads(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()

	var created model.Campaign
	resp := postJSON(t, srv, "/v1/campaigns/", map[string]string{
		"name": "test", "project_id": "p1", "tenant_id": "t1", "kb_version_id": "kb1",
	})
	decode(t, resp, &created)

	resp2 := postJSON(t, srv, "/v1/campaigns/"+created.ID+"/leads", map[string][]string{
		"lead_ids": {"lead-1", "lead-2"},
	})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
}

func TestLaunchCampaign_PreflightFails_UnapprovedKB(t *testing.T) {
	srv, s := newTestServer()
	defer srv.Close()

	// Seed KB with draft status (not approved).
	s.SeedKbVersion(&model.KbVersion{ID: "kb-draft", Status: "draft"})

	var created model.Campaign
	resp := postJSON(t, srv, "/v1/campaigns/", map[string]any{
		"name":              "RE Campaign",
		"project_id":        "p1",
		"tenant_id":         "t1",
		"kb_version_id":     "kb-draft",
		"script_version_id": "v1",
		"prompt_version_id": "v1",
		"source_filter":     "phone",
		"schedule":          "09:00-21:00",
	})
	decode(t, resp, &created)

	// Add required script + prompt versions.
	s.SeedScript(&model.CampaignScript{CampaignID: created.ID, Version: "v1", Content: "script"})
	s.SeedPrompt(&model.CampaignPromptVersion{CampaignID: created.ID, Version: "v1", Prompt: "prompt"})

	resp2 := postJSON(t, srv, "/v1/campaigns/"+created.ID+"/launch", nil)
	if resp2.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unapproved KB, got %d", resp2.StatusCode)
	}
}

func TestSetAndGetLimits(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()

	var created model.Campaign
	resp := postJSON(t, srv, "/v1/campaigns/", map[string]string{
		"name": "test", "project_id": "p1", "tenant_id": "t1", "kb_version_id": "kb1",
	})
	decode(t, resp, &created)

	resp2 := putJSON(t, srv, "/v1/campaigns/"+created.ID+"/limits", map[string]any{
		"daily_call_cap":   100,
		"retry_max":        3,
		"cost_cap_inr":     5000,
		"max_call_seconds": 300,
	})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
}

func putJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func TestPauseThenResumeCampaign(t *testing.T) {
	srv, s := newTestServer()
	defer srv.Close()

	var created model.Campaign
	resp := postJSON(t, srv, "/v1/campaigns/", map[string]any{
		"name":              "pause-test",
		"project_id":        "p1",
		"tenant_id":         "t1",
		"kb_version_id":     "kb-approved",
		"script_version_id": "v1",
		"prompt_version_id": "v1",
		"source_filter":     "phone",
		"schedule":          "09:00-21:00",
	})
	decode(t, resp, &created)

	// Seed approved KB, script, and prompt.
	s.SeedKbVersion(&model.KbVersion{ID: "kb-approved", Status: "approved"})
	s.SeedScript(&model.CampaignScript{CampaignID: created.ID, Version: "v1", Content: "s"})
	s.SeedPrompt(&model.CampaignPromptVersion{CampaignID: created.ID, Version: "v1", Prompt: "p"})

	// Launch.
	launchResp := postJSON(t, srv, "/v1/campaigns/"+created.ID+"/launch", nil)
	if launchResp.StatusCode != http.StatusOK {
		var errBody map[string]string
		decode(t, launchResp, &errBody)
		t.Fatalf("expected launch success, got %d: %v", launchResp.StatusCode, errBody)
	}

	// Pause.
	pauseResp := postJSON(t, srv, "/v1/campaigns/"+created.ID+"/pause", map[string]string{"reason": "test pause"})
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 pause, got %d", pauseResp.StatusCode)
	}

	// Resume.
	resumeResp := postJSON(t, srv, "/v1/campaigns/"+created.ID+"/resume", nil)
	if resumeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 resume, got %d", resumeResp.StatusCode)
	}

	// Verify active.
	fetched := getJSON(t, srv, "/v1/campaigns/"+created.ID)
	var c model.Campaign
	decode(t, fetched, &c)
	if c.Status != model.StatusActive {
		t.Fatalf("expected active after resume, got %s", c.Status)
	}
}
