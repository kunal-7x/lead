// Package activities contains Temporal activity implementations.
// Each activity wraps an HTTP call to another Capsy service, making the side
// effects safe to retry independently of the workflow orchestration.
package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lead/services/temporal-workers/internal/model"
)

// PickNextCallActivity asks the scheduler service for the next call attempt
// for a lead/campaign. Returns the scheduled time for the call.
func PickNextCallActivity(ctx context.Context, input model.RetryLadderInput) (*model.RetryLadderResult, error) {
	schedulerURL := envOr("SCHEDULER_ADDR", "http://localhost:8106")
	body, _ := json.Marshal(input)
	resp, err := doPost(ctx, schedulerURL+"/v1/internal/retry-schedule", body)
	if err != nil {
		// Fallback: compute locally using the pure function (no external dep).
		return computeRetryScheduleLocal(input)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return computeRetryScheduleLocal(input)
	}
	var result model.RetryLadderResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return computeRetryScheduleLocal(input)
	}
	return &result, nil
}

// CheckCallingWindowActivity checks whether the calling window allows a call
// right now. Returns the result and the time to retry if not.
func CheckCallingWindowActivity(ctx context.Context, input model.CallingWindowInput) (*model.CallingWindowResult, error) {
	consentURL := envOr("CONSENT_COMPLIANCE_ADDR", "http://localhost:8107")
	body, _ := json.Marshal(input)
	resp, err := doPost(ctx, consentURL+"/v1/internal/calling-window", body)
	if err != nil {
		return computeCallingWindowLocal(input)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return computeCallingWindowLocal(input)
	}
	var result model.CallingWindowResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return computeCallingWindowLocal(input)
	}
	return &result, nil
}

// RecordCostActivity records a billing event via the billing-meter service.
func RecordCostActivity(ctx context.Context, ev model.BillingMeterEvent) error {
	billingURL := envOr("BILLING_METER_ADDR", "http://localhost:8115")
	body, _ := json.Marshal(map[string]any{
		"tenant_id":     ev.TenantID,
		"campaign_id":   ev.CampaignID,
		"type":          "call.completed",
		"quantity":      1,
		"unit":          "call",
		"cost_burn_inr": ev.CostBurnINR,
	})
	resp, err := doPost(ctx, billingURL+"/v1/usage", body)
	if err != nil {
		return fmt.Errorf("record cost: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("billing-meter %d: %s", resp.StatusCode, b)
	}
	return nil
}

// PauseCampaignActivity pauses a campaign via the campaign service.
func PauseCampaignActivity(ctx context.Context, campaignID, reason string) error {
	campaignURL := envOr("CAMPAIGN_ADDR", "http://localhost:8104")
	body, _ := json.Marshal(map[string]string{"reason": reason})
	resp, err := doPost(ctx, fmt.Sprintf("%s/v1/campaigns/%s/pause", campaignURL, campaignID), body)
	if err != nil {
		return fmt.Errorf("pause campaign %s: %w", campaignID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("campaign service %d: %s", resp.StatusCode, b)
	}
	return nil
}

// GetCampaignHealthActivity fetches the current health snapshot for a campaign.
func GetCampaignHealthActivity(ctx context.Context, campaignID string) (model.HealthSnapshot, error) {
	campaignURL := envOr("CAMPAIGN_ADDR", "http://localhost:8104")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/v1/campaigns/%s/health", campaignURL, campaignID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.HealthSnapshot{CampaignID: campaignID}, nil // return empty; workflow will retry
	}
	defer resp.Body.Close()
	var snap model.HealthSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return model.HealthSnapshot{CampaignID: campaignID}, nil
	}
	return snap, nil
}

// SignalSchedulerActivity tells the scheduler to initiate a call attempt for a lead.
func SignalSchedulerActivity(ctx context.Context, leadID, campaignID string, attempt int) error {
	schedulerURL := envOr("SCHEDULER_ADDR", "http://localhost:8106")
	body, _ := json.Marshal(map[string]any{
		"lead_id":        leadID,
		"campaign_id":    campaignID,
		"attempt_number": attempt,
	})
	resp, err := doPost(ctx, schedulerURL+"/v1/internal/schedule-attempt", body)
	if err != nil {
		return fmt.Errorf("signal scheduler: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("scheduler %d: %s", resp.StatusCode, b)
	}
	return nil
}

// --- local fallback implementations ---

func computeRetryScheduleLocal(input model.RetryLadderInput) (*model.RetryLadderResult, error) {
	policy := input.Policy
	if len(policy.Offsets) == 0 {
		policy = model.DefaultRetryPolicy
	}
	max := policy.Max
	if max <= 0 || max > len(policy.Offsets) {
		max = len(policy.Offsets)
	}
	schedule := make([]model.RetryAttempt, max)
	for i := 0; i < max; i++ {
		schedule[i] = model.RetryAttempt{
			AttemptNumber: i + 1,
			ScheduledAt:   input.FirstCallAt.Add(policy.Offsets[i]).Truncate(time.Second),
		}
	}
	return &model.RetryLadderResult{LeadID: input.LeadID, Schedule: schedule}, nil
}

func computeCallingWindowLocal(input model.CallingWindowInput) (*model.CallingWindowResult, error) {
	if input.Timezone == "" {
		input.Timezone = "Asia/Kolkata"
	}
	loc, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return nil, err
	}
	local := input.RequestedAt.In(loc)
	// Parse window_start / window_end.
	ws := parseHHMM(input.WindowStart, local, loc)
	we := parseHHMM(input.WindowEnd, local, loc)
	if !local.Before(we) {
		next := ws.Add(24 * time.Hour)
		return &model.CallingWindowResult{CanProceed: false, WaitUntil: next, ProceedAt: next}, nil
	}
	if local.Before(ws) {
		return &model.CallingWindowResult{CanProceed: false, WaitUntil: ws, ProceedAt: ws}, nil
	}
	return &model.CallingWindowResult{CanProceed: true, ProceedAt: input.RequestedAt}, nil
}

func parseHHMM(hhmm string, ref time.Time, loc *time.Location) time.Time {
	var h, m int
	fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return time.Date(ref.Year(), ref.Month(), ref.Day(), h, m, 0, 0, loc)
}

func doPost(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
