package consent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type OutreachRequest struct {
	TenantID      string
	LeadID        string
	Phone         string
	Channel       string
	CampaignID    string
	CampaignType  string
	RequestedTime time.Time
}

type OutreachResult struct {
	Allowed bool
	Reason  string
}

type Checker interface {
	CheckOutreachAllowed(context.Context, OutreachRequest) (OutreachResult, error)
}

type StaticChecker struct {
	Allowed bool
	Reason  string
}

func (s StaticChecker) CheckOutreachAllowed(context.Context, OutreachRequest) (OutreachResult, error) {
	if s.Allowed {
		return OutreachResult{Allowed: true, Reason: "allowed"}, nil
	}
	if s.Reason == "" {
		return OutreachResult{Allowed: false, Reason: "blocked"}, nil
	}
	return OutreachResult{Allowed: false, Reason: s.Reason}, nil
}

type HTTPChecker struct {
	BaseURL string
	Client  *http.Client
}

func (h HTTPChecker) CheckOutreachAllowed(ctx context.Context, req OutreachRequest) (OutreachResult, error) {
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]any{
		"lead_id":       req.LeadID,
		"phone":         req.Phone,
		"channel":       req.Channel,
		"at_time":       req.RequestedTime.Format(time.RFC3339),
		"campaign_id":   req.CampaignID,
		"campaign_type": req.CampaignType,
	})
	if err != nil {
		return OutreachResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/v1/consent/check-outreach", bytes.NewReader(body))
	if err != nil {
		return OutreachResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.TenantID != "" {
		httpReq.Header.Set("X-Tenant-ID", req.TenantID)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return OutreachResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return OutreachResult{}, fmt.Errorf("consent-compliance returned %s", resp.Status)
	}
	var out OutreachResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return OutreachResult{}, err
	}
	return out, nil
}
