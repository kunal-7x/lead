package claimcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client interface {
	Check(ctx context.Context, req CheckRequest) (CheckResponse, error)
}

type CheckRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	LeadID    string `json:"lead_id,omitempty"`
	Channel   string `json:"channel"`
	Text      string `json:"text"`
}

type Violation struct {
	ClaimID     string  `json:"claim_id,omitempty"`
	ClaimType   string  `json:"claim_type"`
	Reason      string  `json:"reason"`
	ActionTaken string  `json:"action_taken"`
	Score       float32 `json:"score,omitempty"`
}

type CheckResponse struct {
	OK          bool        `json:"ok"`
	ActionTaken string      `json:"action_taken"`
	Violations  []Violation `json:"violations"`
	Rewritten   *string     `json:"rewritten"`
}

type HTTPClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTP(baseURL string) *HTTPClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://knowledge:8110"
	}
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 4 * time.Second},
	}
}

func (c *HTTPClient) Check(ctx context.Context, req CheckRequest) (CheckResponse, error) {
	if strings.TrimSpace(req.Text) == "" {
		return CheckResponse{OK: true, ActionTaken: "none"}, nil
	}
	req.Channel = "whatsapp"
	body, err := json.Marshal(req)
	if err != nil {
		return CheckResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/claim-control/check", bytes.NewReader(body))
	if err != nil {
		return CheckResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CheckResponse{}, err
	}
	defer resp.Body.Close()
	var out CheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CheckResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CheckResponse{}, fmt.Errorf("claim-control status=%d", resp.StatusCode)
	}
	return out, nil
}
