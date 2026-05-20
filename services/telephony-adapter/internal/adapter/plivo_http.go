package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PlivoBaseURL is the Plivo REST API root. Overridable via NewPlivoHTTPWithBase for tests.
const PlivoBaseURL = "https://api.plivo.com/v1"

// plivoHTTPClient is the concrete PlivoHTTPClient implementation backed by api.plivo.com.
type plivoHTTPClient struct {
	baseURL string
	httpc   *http.Client
}

// NewPlivoHTTP returns a PlivoHTTPClient that talks to the real Plivo REST API.
// Pass an http.Client with a sane timeout (default 15s if nil).
func NewPlivoHTTP(httpc *http.Client) PlivoHTTPClient {
	return NewPlivoHTTPWithBase(PlivoBaseURL, httpc)
}

// NewPlivoHTTPWithBase lets tests point the client at an httptest.Server.
func NewPlivoHTTPWithBase(baseURL string, httpc *http.Client) PlivoHTTPClient {
	if httpc == nil {
		httpc = &http.Client{Timeout: 15 * time.Second}
	}
	return &plivoHTTPClient{baseURL: baseURL, httpc: httpc}
}

type plivoCallRequest struct {
	From      string `json:"from"`
	To        string `json:"to"`
	AnswerURL string `json:"answer_url"`
	AnswerMethod string `json:"answer_method,omitempty"`
	HangupURL string `json:"hangup_url,omitempty"`
}

type plivoCallResponse struct {
	APIID       string `json:"api_id"`
	Message     string `json:"message"`
	RequestUUID string `json:"request_uuid"`
	CallUUID    string `json:"call_uuid"`
}

type plivoRecordingResponse struct {
	APIID         string `json:"api_id"`
	RecordingURL  string `json:"recording_url"`
	RecordingID   string `json:"recording_id"`
	RecordingType string `json:"recording_type"`
}

type plivoListResponse[T any] struct {
	APIID   string `json:"api_id"`
	Objects []T    `json:"objects"`
}

type plivoErrorResponse struct {
	APIID   string `json:"api_id"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func basicAuth(authID, authToken string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(authID+":"+authToken))
}

func (c *plivoHTTPClient) do(ctx context.Context, method, path, authID, authToken string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("plivo: marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	url := fmt.Sprintf("%s/Account/%s%s", c.baseURL, authID, path)
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return fmt.Errorf("plivo: new request: %w", err)
	}
	req.Header.Set("Authorization", basicAuth(authID, authToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("plivo: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("plivo: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var perr plivoErrorResponse
		_ = json.Unmarshal(respBody, &perr)
		msg := perr.Error
		if msg == "" {
			msg = perr.Message
		}
		if msg == "" {
			msg = string(respBody)
		}
		return fmt.Errorf("plivo: http %d: %s", resp.StatusCode, msg)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("plivo: decode response: %w", err)
		}
	}
	return nil
}

// CreateCall POSTs an outbound call. Returns the request_uuid (or call_uuid if Plivo returns one).
func (c *plivoHTTPClient) CreateCall(ctx context.Context, authID, authToken, from, to, answerURL string) (string, error) {
	body := plivoCallRequest{
		From:         from,
		To:           to,
		AnswerURL:    answerURL,
		AnswerMethod: "GET",
	}
	var out plivoCallResponse
	if err := c.do(ctx, http.MethodPost, "/Call/", authID, authToken, body, &out); err != nil {
		return "", err
	}
	if out.CallUUID != "" {
		return out.CallUUID, nil
	}
	if out.RequestUUID != "" {
		return out.RequestUUID, nil
	}
	return "", fmt.Errorf("plivo: CreateCall returned empty uuid (msg=%q)", out.Message)
}

// HangupCall ends an in-progress call via DELETE.
func (c *plivoHTTPClient) HangupCall(ctx context.Context, authID, authToken, callUUID string) error {
	return c.do(ctx, http.MethodDelete, "/Call/"+callUUID+"/", authID, authToken, nil, nil)
}

// TransferCall redirects an active call's A-leg to a new Answer URL that bridges to target.
func (c *plivoHTTPClient) TransferCall(ctx context.Context, authID, authToken, callUUID, target string) error {
	type transferReq struct {
		Legs    string `json:"legs"`
		AlegURL string `json:"aleg_url"`
	}
	body := transferReq{Legs: "aleg", AlegURL: target}
	return c.do(ctx, http.MethodPost, "/Call/"+callUUID+"/", authID, authToken, body, nil)
}

// GetRecordingURL fetches the latest recording for a call_uuid.
func (c *plivoHTTPClient) GetRecordingURL(ctx context.Context, authID, authToken, callUUID string) (string, error) {
	var out plivoListResponse[plivoRecordingResponse]
	if err := c.do(ctx, http.MethodGet, "/Call/"+callUUID+"/Recording/", authID, authToken, nil, &out); err != nil {
		return "", err
	}
	if len(out.Objects) == 0 {
		return "", fmt.Errorf("plivo: no recordings for call_uuid=%s", callUUID)
	}
	return out.Objects[0].RecordingURL, nil
}

// CheckHealth pings the Plivo account endpoint unauthenticated-fast: a GET that always exists.
// We use the account info endpoint with the credentials we already have via env.
func (c *plivoHTTPClient) CheckHealth(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	// Any non-5xx response (including 401 for unauthenticated probe) proves the API is reachable.
	return resp.StatusCode < 500
}
