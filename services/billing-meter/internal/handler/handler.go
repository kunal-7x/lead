package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lead/services/billing-meter/internal/model"
	"github.com/lead/services/billing-meter/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/billing/usage", h.handleUsage)
	mux.HandleFunc("GET /v1/billing/usage", h.handleGetUsage)
	mux.HandleFunc("GET /v1/billing/cost-summary", h.handleGetCostSummary)
	mux.HandleFunc("POST /v1/billing/caps", h.handleSetCaps)
	mux.HandleFunc("GET /v1/billing/balance", h.handleBalance)
	mux.HandleFunc("POST /v1/billing/charge", h.handleCharge)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) handleUsage(w http.ResponseWriter, r *http.Request) {
	var req model.UsageEvent
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	cost, err := h.service.RecordUsage(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cost)
}

func (h *Handler) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.GetUsage(r.Context(), r.URL.Query().Get("tenant_id"), r.URL.Query().Get("group_by"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) handleGetCostSummary(w http.ResponseWriter, r *http.Request) {
	scopeType := r.URL.Query().Get("scope_type")
	if scopeType == "" {
		scopeType = "tenant"
	}
	scopeID := r.URL.Query().Get("tenant_id")
	if scopeType == "campaign" {
		scopeID = r.URL.Query().Get("campaign_id")
	}
	summary, err := h.service.GetCostSummary(r.Context(), scopeType, scopeID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) handleSetCaps(w http.ResponseWriter, r *http.Request) {
	var req model.Caps
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.service.SetCaps(r.Context(), req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleBalance(w http.ResponseWriter, r *http.Request) {
	balance, err := h.service.GetBalance(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance_inr": balance})
}

func (h *Handler) handleCharge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		Amount   string `json:"amount"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	amount, _ := strconv.ParseFloat(req.Amount, 64)
	entry, err := h.service.Charge(r.Context(), req.TenantID, amount, req.Reason)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
