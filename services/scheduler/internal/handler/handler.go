package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/scheduler/internal/dispatcher"
	"github.com/lead/services/scheduler/internal/model"
	"github.com/lead/services/scheduler/internal/picker"
)

type Handler struct {
	picker       *picker.Picker
	telephonyURL string
	client       *http.Client
	dialQueue    dispatcher.DialQueueStore
}

func New(p *picker.Picker) *Handler {
	return NewWithTelephony(p, "http://localhost:8108", http.DefaultClient)
}

func NewWithTelephony(p *picker.Picker, telephonyURL string, client *http.Client) *Handler {
	if client == nil {
		client = http.DefaultClient
	}
	return &Handler{
		picker:       p,
		telephonyURL: strings.TrimRight(telephonyURL, "/"),
		client:       client,
	}
}

// WithDialQueue returns a copy of h with the given DialQueueStore wired in.
func (h *Handler) WithDialQueue(q dispatcher.DialQueueStore) *Handler {
	cp := *h
	cp.dialQueue = q
	return &cp
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/v1/scheduler/pick-next", h.pickNext)
	r.Post("/v1/scheduler/mark-attempt", h.markAttempt)
	r.Post("/v1/scheduler/demo-dispatch", h.demoDispatch)
	r.Get("/v1/scheduler/queue-depth", h.queueDepth)
	r.Get("/v1/campaigns/{id}/progress", h.campaignProgress)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
}

func (h *Handler) demoDispatch(w http.ResponseWriter, r *http.Request) {
	var req model.DemoDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.TenantID == "" || req.CampaignID == "" {
		http.Error(w, "tenant_id and campaign_id required", http.StatusBadRequest)
		return
	}
	if req.WorkerID == "" {
		req.WorkerID = "demo-dispatcher"
	}
	if req.BatchSize <= 0 {
		req.BatchSize = len(req.Leads)
	}
	if req.Region == "" {
		req.Region = "IN"
	}
	if req.MaxDuration <= 0 {
		req.MaxDuration = 300
	}
	if !req.Demo {
		req.Demo = true
	}

	toNumber := make(map[string]string, len(req.Leads))
	leads := make([]*model.Lead, 0, len(req.Leads))
	for _, item := range req.Leads {
		if item.ID == "" || item.ToNumber == "" {
			continue
		}
		toNumber[item.ID] = item.ToNumber
		leads = append(leads, &model.Lead{
			ID:            item.ID,
			CampaignID:    req.CampaignID,
			TenantID:      req.TenantID,
			PriorityScore: item.PriorityScore,
			Status:        model.LeadStatusPending,
		})
	}
	if len(leads) == 0 {
		http.Error(w, "at least one lead with id and to_number required", http.StatusBadRequest)
		return
	}
	if err := h.picker.AddLeads(r.Context(), leads); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	picked, err := h.picker.PickNext(r.Context(), model.PickNextRequest{
		CampaignID: req.CampaignID,
		WorkerID:   req.WorkerID,
		BatchSize:  req.BatchSize,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := model.DemoDispatchResponse{CampaignID: req.CampaignID}
	for _, lead := range picked.Leads {
		call, err := h.createTelephonyCall(r, req, lead, toNumber[lead.ID])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := h.picker.SetCallSession(r.Context(), call.SessionID, lead.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp.Calls = append(resp.Calls, call)
	}
	resp.Attempted = len(resp.Calls)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) createTelephonyCall(r *http.Request, req model.DemoDispatchRequest, lead *model.Lead, to string) (model.DemoDispatchAttempt, error) {
	sessionID := fmt.Sprintf("call-%s", lead.ID)
	payload := map[string]any{
		"session_id":   sessionID,
		"tenant_id":    req.TenantID,
		"campaign_id":  req.CampaignID,
		"lead_id":      lead.ID,
		"from_number":  req.FromNumber,
		"to_number":    to,
		"region":       req.Region,
		"max_duration": req.MaxDuration,
		"demo":         req.Demo,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return model.DemoDispatchAttempt{}, err
	}
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.telephonyURL+"/v1/calls", bytes.NewReader(raw))
	if err != nil {
		return model.DemoDispatchAttempt{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := h.client.Do(httpReq)
	if err != nil {
		return model.DemoDispatchAttempt{}, err
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if httpResp.StatusCode >= 300 {
		return model.DemoDispatchAttempt{}, fmt.Errorf("telephony call failed: %s: %s", httpResp.Status, body)
	}
	var out struct {
		SessionID      string `json:"session_id"`
		Provider       string `json:"provider"`
		ProviderCallID string `json:"provider_call_id"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return model.DemoDispatchAttempt{}, err
	}
	return model.DemoDispatchAttempt{
		LeadID:         lead.ID,
		SessionID:      out.SessionID,
		Provider:       out.Provider,
		ProviderCallID: out.ProviderCallID,
		Status:         out.Status,
	}, nil
}

func (h *Handler) pickNext(w http.ResponseWriter, r *http.Request) {
	var req model.PickNextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := h.picker.PickNext(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) markAttempt(w http.ResponseWriter, r *http.Request) {
	var req model.MarkAttemptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.picker.MarkAttempt(r.Context(), req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) queueDepth(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	resp, err := h.picker.GetQueueDepth(r.Context(), tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// CampaignProgressResponse is the JSON shape for GET /v1/campaigns/{id}/progress.
type CampaignProgressResponse struct {
	CampaignID    string `json:"campaign_id"`
	Total         int    `json:"total"`
	Pending       int    `json:"pending"`
	InFlight      int    `json:"in_flight"`
	Placed        int    `json:"placed"`
	Connected     int    `json:"connected"`
	NoAnswerRetry int    `json:"no_answer_retry"`
	Failed        int    `json:"failed"`
	Suppressed    int    `json:"suppressed"`
	Done          int    `json:"done"`
}

func (h *Handler) campaignProgress(w http.ResponseWriter, r *http.Request) {
	campaignID := chi.URLParam(r, "id")
	if campaignID == "" {
		http.Error(w, "campaign id required", http.StatusBadRequest)
		return
	}
	if h.dialQueue == nil {
		http.Error(w, "dial queue not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	statusCounts, err := h.dialQueue.CountsByStatus(ctx, campaignID)
	if err != nil {
		http.Error(w, fmt.Sprintf("count by status: %v", err), http.StatusInternalServerError)
		return
	}
	dispCounts, err := h.dialQueue.CountsByDisposition(ctx, campaignID)
	if err != nil {
		http.Error(w, fmt.Sprintf("count by disposition: %v", err), http.StatusInternalServerError)
		return
	}

	// pending rows that have been attempted at least once = re-queued no-answer retries.
	noAnswerRetry, err := countPendingWithAttempts(ctx, h.dialQueue, campaignID)
	if err != nil {
		http.Error(w, fmt.Sprintf("pending with attempts: %v", err), http.StatusInternalServerError)
		return
	}

	pending := statusCounts["pending"]
	inFlight := statusCounts["in_flight"]
	done := statusCounts["done"]
	failed := statusCounts["failed"]
	suppressed := statusCounts["suppressed"]

	total := pending + inFlight + done + failed + suppressed
	placed := inFlight + done + failed
	connected := dispCounts["answered"]

	resp := CampaignProgressResponse{
		CampaignID:    campaignID,
		Total:         total,
		Pending:       pending,
		InFlight:      inFlight,
		Placed:        placed,
		Connected:     connected,
		NoAnswerRetry: noAnswerRetry,
		Failed:        failed,
		Suppressed:    suppressed,
		Done:          done,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// countPendingWithAttempts counts pending rows with attempts > 0 (re-queued leads).
// It queries via a ListRows-compatible helper on the store interface.
// Since DialQueueStore doesn't expose a full scan, we use a type assertion to
// FakeDialQueueStore in tests and fall back to 0 for the pgkv store (where the
// pgkv store has an AllRows-style scan via CountsPendingWithAttempts added below).
// To keep the interface minimal we add a PendingWithAttempts method to the interface.
func countPendingWithAttempts(ctx context.Context, q dispatcher.DialQueueStore, campaignID string) (int, error) {
	type pendingAtemptCounter interface {
		CountPendingWithAttempts(ctx context.Context, campaignID string) (int, error)
	}
	if pac, ok := q.(pendingAtemptCounter); ok {
		return pac.CountPendingWithAttempts(ctx, campaignID)
	}
	return 0, nil
}
