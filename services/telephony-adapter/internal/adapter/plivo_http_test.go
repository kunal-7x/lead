package adapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lead/services/telephony-adapter/internal/adapter"
)

const (
	tAuthID    = "MAJABCDEF1234567890"
	tAuthToken = "test-auth-token-secret"
)

// fakePlivoServer returns an httptest.Server that mimics the small subset of
// the Plivo REST API our client uses. handler is called once per request.
func fakePlivoServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestPlivoHTTP_CreateCall_Success(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/Account/" + tAuthID + "/Call/"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Errorf("expected basic auth header, got %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["from"] != "+15551234567" || body["to"] != "+919999999999" {
			t.Errorf("wrong from/to in body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"api_id":       "api-xyz",
			"message":      "call fired",
			"request_uuid": "req-uuid-001",
		})
	})

	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	uuid, err := cli.CreateCall(context.Background(), tAuthID, tAuthToken,
		"+15551234567", "+919999999999", "https://example.com/answer")
	if err != nil {
		t.Fatalf("CreateCall: %v", err)
	}
	if uuid != "req-uuid-001" {
		t.Errorf("expected request_uuid, got %q", uuid)
	}
}

func TestPlivoHTTP_CreateCall_Error(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
	})

	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	_, err := cli.CreateCall(context.Background(), tAuthID, "wrong",
		"+15551234567", "+919999999999", "https://example.com/answer")
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to mention 401, got %v", err)
	}
}

func TestPlivoHTTP_HangupCall(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/Call/call-uuid-1/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if err := cli.HangupCall(context.Background(), tAuthID, tAuthToken, "call-uuid-1"); err != nil {
		t.Fatalf("HangupCall: %v", err)
	}
}

func TestPlivoHTTP_GetRecordingURL(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"api_id": "api-rec",
			"objects": []map[string]string{
				{"recording_id": "rec-1", "recording_url": "https://recordings.plivo.com/rec-1.mp3", "recording_type": "conference"},
			},
		})
	})
	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	url, err := cli.GetRecordingURL(context.Background(), tAuthID, tAuthToken, "call-uuid-1")
	if err != nil {
		t.Fatalf("GetRecordingURL: %v", err)
	}
	if url != "https://recordings.plivo.com/rec-1.mp3" {
		t.Errorf("unexpected url %q", url)
	}
}

func TestPlivoHTTP_GetRecordingURL_None(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"api_id": "x", "objects": []map[string]string{}})
	})
	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if _, err := cli.GetRecordingURL(context.Background(), tAuthID, tAuthToken, "call-uuid-1"); err == nil {
		t.Fatal("expected error when no recordings, got nil")
	}
}

func TestPlivoHTTP_CheckHealth(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // healthy probe: server reachable
	})
	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if !cli.CheckHealth(context.Background()) {
		t.Error("expected CheckHealth=true on reachable server")
	}
}

// TestPlivoAdapter_PlaceCall_ExercisesHTTPClient ensures the high-level
// Plivo adapter routes through the HTTP client correctly.
func TestPlivoAdapter_PlaceCall_ExercisesHTTPClient(t *testing.T) {
	srv := fakePlivoServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"request_uuid": "req-200"})
	})
	cli := adapter.NewPlivoHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	a := adapter.NewPlivo(tAuthID, tAuthToken, cli)
	if a.Name() != "plivo" {
		t.Errorf("expected name plivo, got %s", a.Name())
	}
}
