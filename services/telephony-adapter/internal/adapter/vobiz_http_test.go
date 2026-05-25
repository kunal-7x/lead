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
	vAuthID    = "VBZABC1234567890"
	vAuthToken = "vobiz-test-auth-token"
)

// fakeVobizServer returns an httptest.Server that mimics the Vobiz REST API.
func fakeVobizServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// assertVobizAuth verifies that the request carries the expected Vobiz header auth.
func assertVobizAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Auth-ID"); got != vAuthID {
		t.Errorf("X-Auth-ID: want %q, got %q", vAuthID, got)
	}
	if got := r.Header.Get("X-Auth-Token"); got != vAuthToken {
		t.Errorf("X-Auth-Token: want %q, got %q", vAuthToken, got)
	}
	// Must NOT use HTTP Basic Auth (that's the Plivo pattern, not Vobiz).
	if got := r.Header.Get("Authorization"); got != "" {
		t.Errorf("expected no Authorization header for Vobiz, got %q", got)
	}
}

func TestVobizHTTP_CreateCall_Success_RequestUUID(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/Account/" + vAuthID + "/Call/"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		assertVobizAuth(t, r)

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["from"] != "+15551234567" || body["to"] != "+919999999999" {
			t.Errorf("wrong from/to in body: %v", body)
		}
		if body["answer_url"] != "https://example.com/answer" {
			t.Errorf("wrong answer_url in body: %v", body)
		}
		if body["answer_method"] != "POST" {
			t.Errorf("expected answer_method=POST, got %v", body["answer_method"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"api_id":       "api-vbz-001",
			"message":      "call initiated",
			"request_uuid": "req-uuid-vbz-001",
		})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	uuid, err := cli.CreateCall(context.Background(), vAuthID, vAuthToken,
		"+15551234567", "+919999999999", "https://example.com/answer")
	if err != nil {
		t.Fatalf("CreateCall: %v", err)
	}
	if uuid != "req-uuid-vbz-001" {
		t.Errorf("expected request_uuid, got %q", uuid)
	}
}

func TestVobizHTTP_CreateCall_Success_MessageUUID(t *testing.T) {
	// Vobiz may return message_uuid instead of request_uuid — we must handle both.
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertVobizAuth(t, r)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"api_id":       "api-vbz-002",
			"message_uuid": "msg-uuid-vbz-002",
		})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	uuid, err := cli.CreateCall(context.Background(), vAuthID, vAuthToken,
		"+15551234567", "+919999999999", "https://example.com/answer")
	if err != nil {
		t.Fatalf("CreateCall: %v", err)
	}
	if uuid != "msg-uuid-vbz-002" {
		t.Errorf("expected message_uuid fallback, got %q", uuid)
	}
}

func TestVobizHTTP_CreateCall_Success_CallUUID(t *testing.T) {
	// Some Plivo-family APIs return call_uuid directly.
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertVobizAuth(t, r)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"call_uuid": "call-uuid-direct-003",
		})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	uuid, err := cli.CreateCall(context.Background(), vAuthID, vAuthToken,
		"+15551234567", "+919999999999", "https://example.com/answer")
	if err != nil {
		t.Fatalf("CreateCall: %v", err)
	}
	if uuid != "call-uuid-direct-003" {
		t.Errorf("expected call_uuid fallback, got %q", uuid)
	}
}

func TestVobizHTTP_CreateCall_Error(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Wrong token is intentional here — verify the headers are sent (even if wrong)
		// and that the client correctly propagates the 401 error.
		if r.Header.Get("X-Auth-ID") == "" {
			t.Error("expected X-Auth-ID header to be present")
		}
		if r.Header.Get("X-Auth-Token") == "" {
			t.Error("expected X-Auth-Token header to be present")
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header for Vobiz, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	_, err := cli.CreateCall(context.Background(), vAuthID, "wrong-token",
		"+15551234567", "+919999999999", "https://example.com/answer")
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to mention 401, got %v", err)
	}
}

func TestVobizHTTP_CreateCall_EmptyUUID(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"api_id": "api-x", "message": "ok"})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	_, err := cli.CreateCall(context.Background(), vAuthID, vAuthToken,
		"+1555", "+1556", "https://example.com/answer")
	if err == nil {
		t.Fatal("expected error when all UUID fields are empty")
	}
	if !strings.Contains(err.Error(), "empty uuid") {
		t.Errorf("expected 'empty uuid' in error, got %v", err)
	}
}

func TestVobizHTTP_HangupCall(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		wantPath := "/Account/" + vAuthID + "/Call/call-uuid-1/"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		assertVobizAuth(t, r)
		w.WriteHeader(http.StatusNoContent)
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if err := cli.HangupCall(context.Background(), vAuthID, vAuthToken, "call-uuid-1"); err != nil {
		t.Fatalf("HangupCall: %v", err)
	}
}

func TestVobizHTTP_GetRecordingURL(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/Account/" + vAuthID + "/Call/call-uuid-1/Recording/"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		assertVobizAuth(t, r)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"api_id": "api-rec-vbz",
			"objects": []map[string]string{
				{
					"recording_id":   "rec-vbz-1",
					"recording_url":  "https://recordings.vobiz.ai/rec-vbz-1.mp3",
					"recording_type": "mp3",
				},
			},
		})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	url, err := cli.GetRecordingURL(context.Background(), vAuthID, vAuthToken, "call-uuid-1")
	if err != nil {
		t.Fatalf("GetRecordingURL: %v", err)
	}
	if url != "https://recordings.vobiz.ai/rec-vbz-1.mp3" {
		t.Errorf("unexpected recording url %q", url)
	}
}

func TestVobizHTTP_GetRecordingURL_None(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"api_id": "x", "objects": []map[string]string{}})
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if _, err := cli.GetRecordingURL(context.Background(), vAuthID, vAuthToken, "call-uuid-1"); err == nil {
		t.Fatal("expected error when no recordings, got nil")
	}
}

func TestVobizHTTP_CheckHealth_Reachable(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 401 = server is up but credentials not validated; still counts as healthy.
		w.WriteHeader(http.StatusUnauthorized)
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if !cli.CheckHealth(context.Background(), vAuthID) {
		t.Error("expected CheckHealth=true for reachable server with 401")
	}
}

func TestVobizHTTP_CheckHealth_ServerError(t *testing.T) {
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if cli.CheckHealth(context.Background(), vAuthID) {
		t.Error("expected CheckHealth=false for 500 server error")
	}
}

func TestVobizHTTP_CheckHealth_Path(t *testing.T) {
	// Verify the health check hits the correct path: /Account/{authID}/
	srv := fakeVobizServer(t, func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/Account/" + vAuthID + "/"
		if r.URL.Path != wantPath {
			t.Errorf("CheckHealth: expected path %q, got %q", wantPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	cli := adapter.NewVobizHTTPWithBase(srv.URL, &http.Client{Timeout: 2 * time.Second})
	if !cli.CheckHealth(context.Background(), vAuthID) {
		t.Error("expected CheckHealth=true on 200")
	}
}
