package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VobizBaseURL is the Vobiz REST API root. Overridable via NewVobizHTTPWithBase for tests.
// In production, set env VOBIZ_BASE_URL=https://api.vobiz.ai/api/v1.
const VobizBaseURL = "https://api.vobiz.ai/api/v1"

// VobizHTTPClient abstracts the Vobiz REST API for testability.
type VobizHTTPClient interface {
	CreateCall(ctx context.Context, authID, authToken, from, to, answerURL string) (string, error)
	HangupCall(ctx context.Context, authID, authToken, callUUID string) error
	TransferCall(ctx context.Context, authID, authToken, callUUID, target string) error
	GetRecordingURL(ctx context.Context, authID, authToken, callUUID string) (string, error)
	CheckHealth(ctx context.Context, authID string) bool
}

// vobizHTTPClient is the concrete VobizHTTPClient implementation backed by api.vobiz.ai.
type vobizHTTPClient struct {
	baseURL string
	httpc   *http.Client
}

// NewVobizHTTP returns a VobizHTTPClient that talks to the real Vobiz REST API.
// Pass an http.Client with a sane timeout (default 15s if nil).
func NewVobizHTTP(httpc *http.Client) VobizHTTPClient {
	return NewVobizHTTPWithBase(VobizBaseURL, httpc)
}

// NewVobizHTTPWithBase lets tests point the client at an httptest.Server.
func NewVobizHTTPWithBase(baseURL string, httpc *http.Client) VobizHTTPClient {
	if httpc == nil {
		httpc = &http.Client{Timeout: 15 * time.Second}
	}
	return &vobizHTTPClient{baseURL: baseURL, httpc: httpc}
}

type vobizCallRequest struct {
	From         string `json:"from"`
	To           string `json:"to"`
	AnswerURL    string `json:"answer_url"`
	AnswerMethod string `json:"answer_method,omitempty"`
}

// vobizCallResponse covers both Plivo-family field names: request_uuid and message_uuid.
// Vobiz appears to be a Plivo-family API; we try request_uuid first, then message_uuid, then api_id.
type vobizCallResponse struct {
	APIID       string `json:"api_id"`
	Message     string `json:"message"`
	RequestUUID string `json:"request_uuid"`
	MessageUUID string `json:"message_uuid"`
	CallUUID    string `json:"call_uuid"`
}

type vobizRecordingObject struct {
	RecordingURL  string `json:"recording_url"`
	RecordingID   string `json:"recording_id"`
	RecordingType string `json:"recording_type"`
}

type vobizListResponse[T any] struct {
	APIID   string `json:"api_id"`
	Objects []T    `json:"objects"`
}

type vobizErrorResponse struct {
	APIID   string `json:"api_id"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

// do executes an authenticated Vobiz API request. Authentication is done via
// X-Auth-ID and X-Auth-Token request headers (NOT HTTP Basic Auth).
func (c *vobizHTTPClient) do(ctx context.Context, method, path, authID, authToken string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("vobiz: marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	url := fmt.Sprintf("%s/Account/%s%s", c.baseURL, authID, path)
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return fmt.Errorf("vobiz: new request: %w", err)
	}
	// Vobiz uses header-based auth instead of HTTP Basic.
	req.Header.Set("X-Auth-ID", authID)
	req.Header.Set("X-Auth-Token", authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("vobiz: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("vobiz: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var verr vobizErrorResponse
		_ = json.Unmarshal(respBody, &verr)
		msg := verr.Error
		if msg == "" {
			msg = verr.Message
		}
		if msg == "" {
			msg = string(respBody)
		}
		return fmt.Errorf("vobiz: http %d: %s", resp.StatusCode, msg)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("vobiz: decode response: %w", err)
		}
	}
	return nil
}

// CreateCall POSTs an outbound call.
// Request shape: POST {baseURL}/Account/{authID}/Call/
// JSON body: {"from":"...","to":"...","answer_url":"...","answer_method":"POST"}
// UUID parsing priority: call_uuid → request_uuid → message_uuid (tolerant of Plivo-family variants).
func (c *vobizHTTPClient) CreateCall(ctx context.Context, authID, authToken, from, to, answerURL string) (string, error) {
	body := vobizCallRequest{
		From:         from,
		To:           to,
		AnswerURL:    answerURL,
		AnswerMethod: "POST",
	}
	var out vobizCallResponse
	if err := c.do(ctx, http.MethodPost, "/Call/", authID, authToken, body, &out); err != nil {
		return "", err
	}
	// Tolerant UUID extraction: try all Plivo-family field names.
	if out.CallUUID != "" {
		return out.CallUUID, nil
	}
	if out.RequestUUID != "" {
		return out.RequestUUID, nil
	}
	if out.MessageUUID != "" {
		return out.MessageUUID, nil
	}
	return "", fmt.Errorf("vobiz: CreateCall returned empty uuid (api_id=%q msg=%q)", out.APIID, out.Message)
}

// HangupCall ends an in-progress call via DELETE.
func (c *vobizHTTPClient) HangupCall(ctx context.Context, authID, authToken, callUUID string) error {
	return c.do(ctx, http.MethodDelete, "/Call/"+callUUID+"/", authID, authToken, nil, nil)
}

// TransferCall redirects an active call's A-leg to a new answer URL that bridges to target.
// NOTE: Vobiz is a Plivo-family API; we mirror Plivo's transfer shape (POST with legs+aleg_url).
// If Vobiz diverges here, update with provider-specific payload.
func (c *vobizHTTPClient) TransferCall(ctx context.Context, authID, authToken, callUUID, target string) error {
	type transferReq struct {
		Legs    string `json:"legs"`
		AlegURL string `json:"aleg_url"`
	}
	body := transferReq{Legs: "aleg", AlegURL: target}
	return c.do(ctx, http.MethodPost, "/Call/"+callUUID+"/", authID, authToken, body, nil)
}

// GetRecordingURL fetches the latest recording URL for a call_uuid.
func (c *vobizHTTPClient) GetRecordingURL(ctx context.Context, authID, authToken, callUUID string) (string, error) {
	var out vobizListResponse[vobizRecordingObject]
	if err := c.do(ctx, http.MethodGet, "/Call/"+callUUID+"/Recording/", authID, authToken, nil, &out); err != nil {
		return "", err
	}
	if len(out.Objects) == 0 {
		return "", fmt.Errorf("vobiz: no recordings for call_uuid=%s", callUUID)
	}
	return out.Objects[0].RecordingURL, nil
}

// CheckHealth probes the Vobiz account endpoint. A 2xx response means the API is reachable.
// A 401 (unauthenticated) from the root also means the API is up — we return true for any non-5xx.
func (c *vobizHTTPClient) CheckHealth(ctx context.Context, authID string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/Account/%s/", c.baseURL, authID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode < 500
}
