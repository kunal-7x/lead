// Package handler wires the HTTP router for the telephony adapter service.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/telephony-adapter/internal/model"
	"github.com/lead/services/telephony-adapter/internal/routing"
	"github.com/lead/services/telephony-adapter/internal/webhook"
)

// Handler holds all HTTP handlers.
type Handler struct {
	wh     *webhook.Handler
	router *routing.Router
}

func New(wh *webhook.Handler, r *routing.Router) *Handler {
	return &Handler{wh: wh, router: r}
}

// Routes returns the fully-configured chi router.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", h.healthz)
	r.Post("/wh/plivo/{event}", h.wh.ServeHTTP)
	r.Post("/v1/calls", h.createCall)
	r.Get("/v1/telephony/providers", h.listProviders)
	return r
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) listProviders(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	a, err := h.router.Select(r.Context(), region)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"provider": a.Name()})
}

type createCallRequest struct {
	SessionID   string `json:"session_id"`
	TenantID    string `json:"tenant_id"`
	CampaignID  string `json:"campaign_id"`
	LeadID      string `json:"lead_id"`
	ContactID   string `json:"contact_id"`
	ProjectID   string `json:"project_id"`
	FromNumber  string `json:"from_number"`
	ToNumber    string `json:"to_number"`
	CallbackURL string `json:"callback_url"`
	Region      string `json:"region"`
	MaxDuration int    `json:"max_duration"`
	Demo        bool   `json:"demo"`
}

func (h *Handler) createCall(w http.ResponseWriter, r *http.Request) {
	var req createCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.TenantID == "" {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	if req.ToNumber == "" {
		http.Error(w, "to_number required", http.StatusBadRequest)
		return
	}

	session, err := h.router.PlaceCall(r.Context(), model.CallRequest{
		SessionID:   req.SessionID,
		TenantID:    req.TenantID,
		FromNumber:  req.FromNumber,
		ToNumber:    req.ToNumber,
		CallbackURL: req.CallbackURL,
		Region:      req.Region,
		MaxDuration: req.MaxDuration,
		Demo:        req.Demo,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id":       session.ID,
		"tenant_id":        session.TenantID,
		"campaign_id":      req.CampaignID,
		"lead_id":          req.LeadID,
		"contact_id":       req.ContactID,
		"project_id":       req.ProjectID,
		"provider":         session.ProviderName,
		"provider_call_id": session.ProviderCallID,
		"status":           session.Status,
	})
}
