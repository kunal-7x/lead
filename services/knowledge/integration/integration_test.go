//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lead/services/knowledge/internal/handler"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := store.NewFake()
	h := handler.New(s)
	return httptest.NewServer(h.Router())
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	return resp
}

func getJSON(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	require.NoError(t, err)
	return resp
}

func putJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPut, srv.URL+path, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
}

func TestIntegration_CreateAndGetProject(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	// Create project
	resp := postJSON(t, srv, "/v1/knowledge/projects", map[string]string{
		"tenant_id": "tenant-1",
		"name":      "Integration Project",
	})
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created model.Project
	decodeJSON(t, resp, &created)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "Integration Project", created.Name)

	// Get project
	resp2 := getJSON(t, srv, "/v1/knowledge/projects/"+created.ID)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var fetched model.Project
	decodeJSON(t, resp2, &fetched)
	assert.Equal(t, created.ID, fetched.ID)
}

func TestIntegration_ListProjects(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	postJSON(t, srv, "/v1/knowledge/projects", map[string]string{
		"tenant_id": "tenant-list",
		"name":      "Proj A",
	})
	postJSON(t, srv, "/v1/knowledge/projects", map[string]string{
		"tenant_id": "tenant-list",
		"name":      "Proj B",
	})

	resp := getJSON(t, srv, "/v1/knowledge/projects?tenant_id=tenant-list")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var projects []*model.Project
	decodeJSON(t, resp, &projects)
	assert.Len(t, projects, 2)
}

func TestIntegration_CreateKbVersionAndAddContent(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	// Create project
	resp := postJSON(t, srv, "/v1/knowledge/projects", map[string]string{
		"tenant_id": "tenant-2",
		"name":      "Content Project",
	})
	var project model.Project
	decodeJSON(t, resp, &project)

	// Create KB version
	resp2 := postJSON(t, srv, "/v1/knowledge/projects/"+project.ID+"/kb-versions", map[string]string{})
	assert.Equal(t, http.StatusCreated, resp2.StatusCode)
	var version model.KbVersion
	decodeJSON(t, resp2, &version)
	assert.Equal(t, model.VersionDraft, version.Status)

	// Add a fact
	resp3 := postJSON(t, srv, "/v1/knowledge/versions/"+version.ID+"/facts", map[string]string{
		"content": "The project has 200 units",
	})
	assert.Equal(t, http.StatusCreated, resp3.StatusCode)

	// Add a FAQ
	resp4 := postJSON(t, srv, "/v1/knowledge/versions/"+version.ID+"/faqs", map[string]string{
		"question": "What is the price?",
		"answer":   "Starts at 50L",
	})
	assert.Equal(t, http.StatusCreated, resp4.StatusCode)
}

func TestIntegration_ApprovalWorkflow(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	// Create project with all required fields
	resp := postJSON(t, srv, "/v1/knowledge/projects", map[string]any{
		"tenant_id":           "tenant-wf",
		"name":                "Workflow Project",
		"rera_number":         "RERA-WF-001",
		"brochure_asset_id":   "brochure-wf",
		"price_sheet_asset_id": "ps-wf",
	})
	var project model.Project
	decodeJSON(t, resp, &project)

	// Create version
	resp2 := postJSON(t, srv, "/v1/knowledge/projects/"+project.ID+"/kb-versions", map[string]string{})
	var version model.KbVersion
	decodeJSON(t, resp2, &version)

	// Submit
	resp3 := postJSON(t, srv, "/v1/knowledge/versions/"+version.ID+"/submit", map[string]string{
		"actor_id": "submitter-1",
	})
	assert.Equal(t, http.StatusOK, resp3.StatusCode)

	// Approve
	resp4 := postJSON(t, srv, "/v1/knowledge/versions/"+version.ID+"/approve", map[string]string{
		"reviewer_id": "reviewer-1",
	})
	assert.Equal(t, http.StatusOK, resp4.StatusCode)

	// Publish
	resp5 := postJSON(t, srv, "/v1/knowledge/versions/"+version.ID+"/publish", map[string]string{
		"actor_id": "actor-1",
	})
	assert.Equal(t, http.StatusOK, resp5.StatusCode)
}

func TestIntegration_Pronunciations(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	resp := postJSON(t, srv, "/v1/knowledge/pronunciations", map[string]string{
		"tenant_id": "tenant-p",
		"term":      "Andheri",
		"ipa":       "ˈændəri",
		"phonetic":  "AN-duh-ree",
		"lang":      "en",
	})
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	resp2 := getJSON(t, srv, "/v1/knowledge/pronunciations?tenant_id=tenant-p&lang=en")
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var list []*model.Pronunciation
	decodeJSON(t, resp2, &list)
	assert.Len(t, list, 1)
	assert.Equal(t, "Andheri", list[0].Term)
}

func TestIntegration_UpdateProject(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	resp := postJSON(t, srv, "/v1/knowledge/projects", map[string]string{
		"tenant_id": "tenant-upd",
		"name":      "Before Update",
	})
	var project model.Project
	decodeJSON(t, resp, &project)

	project.Name = "After Update"
	project.RERANumber = "RERA-UPD"
	resp2 := putJSON(t, srv, "/v1/knowledge/projects/"+project.ID, project)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	resp3 := getJSON(t, srv, "/v1/knowledge/projects/"+project.ID)
	var updated model.Project
	decodeJSON(t, resp3, &updated)
	assert.Equal(t, "After Update", updated.Name)
}
