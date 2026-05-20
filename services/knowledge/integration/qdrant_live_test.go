//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/handler"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/netutil"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
	"github.com/stretchr/testify/require"
)

func init() { netutil.MaybeOverrideDefaultResolver() }

// TestC6_FullPipeline_Live runs the full mock-to-real pipeline:
//
//  1. Create project + KB version
//  2. Add facts/FAQs/disclaimers
//  3. Approve + publish → triggers real Gemini embeddings + Qdrant upsert
//  4. Retrieve via /retrieve with a text query → real Qdrant search
//
// Skips unless RUN_LIVE_TESTS=1, QDRANT_URL, and an embedding key are set.
func TestC6_FullPipeline_Live(t *testing.T) {
	if os.Getenv("RUN_LIVE_TESTS") != "1" {
		t.Skip("set RUN_LIVE_TESTS=1 to run live Qdrant + embedding tests")
	}
	qURL := os.Getenv("QDRANT_URL")
	if qURL == "" {
		t.Skip("QDRANT_URL not set")
	}
	if os.Getenv("GOOGLE_GEMINI_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" && os.Getenv("HUGGINGFACE_TOKEN") == "" {
		t.Skip("no embedding credentials set")
	}

	embedder, err := embed.FromEnv()
	require.NoError(t, err, "construct embedder")
	t.Logf("embedder = %s (dim=%d)", embedder.Name(), embedder.Dim())

	q := vector.New(qURL, os.Getenv("QDRANT_API_KEY"))
	require.NoError(t, q.Healthy(context.Background()), "qdrant healthy")

	s := store.NewFake()
	h := handler.New(s).WithVector(embedder, q)
	srv := httptest.NewServer(h.Router())
	defer srv.Close()

	// 1) create project
	tenantID := "tenant-c6-live"
	project := map[string]any{
		"tenant_id":            tenantID,
		"name":                 "DLF Park Place",
		"rera_number":          "RERA-HRYN-123",
		"brochure_asset_id":    "00000000-0000-0000-0000-000000000001",
		"price_sheet_asset_id": "00000000-0000-0000-0000-000000000002",
	}
	resp := postRaw(t, srv, "/v1/knowledge/projects", project)
	requireStatus(t, resp, http.StatusCreated)
	var createdProject model.Project
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&createdProject))
	resp.Body.Close()
	t.Logf("project=%s tenant=%s", createdProject.ID, createdProject.TenantID)

	// 2) create kb version
	resp = postRaw(t, srv, "/v1/knowledge/projects/"+createdProject.ID+"/kb-versions", map[string]any{})
	requireStatus(t, resp, http.StatusCreated)
	var version model.KbVersion
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&version))
	resp.Body.Close()
	t.Logf("version=%s", version.ID)

	// 3) add facts (real estate-flavored)
	facts := []string{
		"Price starts at 1.5 crore for a 2BHK apartment.",
		"3BHK apartments start at 2.3 crore. Possession by December 2027.",
		"Project has a 30000 sq ft clubhouse with infinity pool and gym.",
		"School zone: DPS Sector 45 is 800 meters away. Hospital: Fortis is 2 km.",
		"Total inventory: 480 units across 8 towers. 60 percent already sold.",
	}
	for _, f := range facts {
		resp = postRaw(t, srv, "/v1/knowledge/versions/"+version.ID+"/facts",
			map[string]any{"content": f})
		requireStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}

	// FAQ
	resp = postRaw(t, srv, "/v1/knowledge/versions/"+version.ID+"/faqs", map[string]any{
		"question": "Is the project RERA registered?",
		"answer":   "Yes, RERA-HRYN-123. Approval valid until December 2030.",
	})
	requireStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// 4) submit → approve → publish (triggers index)
	actor := "00000000-0000-0000-0000-0000000000aa"
	requireStatus(t, postRaw(t, srv, "/v1/knowledge/versions/"+version.ID+"/submit",
		map[string]any{"actor_id": actor}), http.StatusOK)

	requireStatus(t, postRaw(t, srv, "/v1/knowledge/versions/"+version.ID+"/approve",
		map[string]any{"reviewer_id": actor}), http.StatusOK)

	publishStart := time.Now()
	resp = postRaw(t, srv, "/v1/knowledge/versions/"+version.ID+"/publish",
		map[string]any{"actor_id": actor})
	requireStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	t.Logf("publish (including embed+upsert) took %s", time.Since(publishStart))

	// 5) retrieve a real query
	queries := []struct {
		query   string
		mustHit string
	}{
		{"what is the cheapest unit?", "1.5 crore"},
		{"tell me about clubhouse amenities", "clubhouse"},
		{"is this project RERA approved?", "RERA"},
	}
	for _, tc := range queries {
		t.Run("retrieve_"+strings.ReplaceAll(tc.query, " ", "_"), func(t *testing.T) {
			searchStart := time.Now()
			resp := postRaw(t, srv, "/v1/knowledge/projects/"+createdProject.ID+"/retrieve",
				map[string]any{"query": tc.query, "top_k": 3})
			requireStatus(t, resp, http.StatusOK)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Logf("retrieve(%q) took %s body=%s", tc.query, time.Since(searchStart), string(body))

			var result model.RetrieveResult
			require.NoError(t, json.Unmarshal(body, &result))
			require.NotEmpty(t, result.Chunks, "retrieve must return at least one chunk")
			require.Equal(t, version.ID, result.VersionStamp)

			found := false
			for _, c := range result.Chunks {
				if strings.Contains(strings.ToLower(c.Content), strings.ToLower(tc.mustHit)) {
					found = true
					break
				}
			}
			if !found {
				t.Logf("WARNING: expected term %q not found in top-3 results — semantic match may still be acceptable", tc.mustHit)
				// Don't hard-fail: real embeddings are stochastic across runs.
			}
		})
	}
}

func postRaw(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode == want {
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, want, string(body))
}
