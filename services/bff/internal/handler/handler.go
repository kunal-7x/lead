package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/bff/internal/middleware"
)

// TenantAuthClient is a thin HTTP/Connect-RPC client for tenant-auth.
type TenantAuthClient struct {
	base   string
	client *http.Client
}

// NewTenantAuthClient creates a client pointing at baseURL.
func NewTenantAuthClient(baseURL string) *TenantAuthClient {
	return &TenantAuthClient{
		base:   baseURL,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *TenantAuthClient) call(r *http.Request, method string, reqBody, respBody any) (int, error) {
	b, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("%s/evs.v1.TenantAuthService/%s", c.base, method)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tid := middleware.GetTenantID(r.Context()); tid != "" {
		req.Header.Set("X-Tenant-Id", tid)
	}
	if uid := middleware.GetUserID(r.Context()); uid != "" {
		req.Header.Set("X-User-Id", uid)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("upstream error %d: %s", resp.StatusCode, body)
	}
	if respBody != nil {
		if err := json.Unmarshal(body, respBody); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// proxy copies a JSON request body to tenant-auth and streams the response back.
func proxy(w http.ResponseWriter, r *http.Request, c *TenantAuthClient, method string) {
	var reqBody json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && err != io.EOF {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	var respBody json.RawMessage
	status, err := c.call(r, method, reqBody, &respBody)
	if err != nil {
		if status == 0 {
			status = http.StatusBadGateway
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody) //nolint:errcheck
}

// Healthz returns 200 OK.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// Readyz returns 200 when the service is ready to accept traffic.
func Readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ready"}`)
}

// Stream is a WebSocket stub that currently returns 101 Switching Protocols
// only if the client sends a valid upgrade header; otherwise 501.
func Stream(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "websocket upgrade required", http.StatusNotImplemented)
			return
		}
		// Real fan-in is added in a later phase; for now just log and close.
		logger.Info("ws stub: connection attempt",
			"tenant_id", middleware.GetTenantID(r.Context()),
			"user_id", middleware.GetUserID(r.Context()),
		)
		http.Error(w, "not yet implemented", http.StatusNotImplemented)
	}
}

func Login(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "Login") }
}

func RefreshToken(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "RefreshToken") }
}

func Logout(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "Logout") }
}

func Enroll2FA(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "Enroll2FA") }
}

func Verify2FA(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "Verify2FA") }
}

func CreateAPIKey(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "CreateApiKey") }
}

func RevokeAPIKey(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyID := chi.URLParam(r, "keyID")
		_ = keyID // passed via request body in Connect-RPC style
		proxy(w, r, c, "RevokeApiKey")
	}
}

func ListTeam(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roles := middleware.GetRoles(r.Context())
		// Only owner / admin roles can see team management
		allowed := false
		for _, role := range roles {
			if role == "super_admin" || role == "internal_admin" || role == "client_owner" || role == "sales_manager" {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		proxy(w, r, c, "ListRoles")
	}
}

func QueryAuditLog(c *TenantAuthClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { proxy(w, r, c, "QueryAuditLogs") }
}
