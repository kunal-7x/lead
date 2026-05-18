package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lead/services/internal-admin-api/internal/model"
	"github.com/lead/services/internal-admin-api/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /v1/internal/tenants", h.handleListTenants)
	mux.HandleFunc("POST /v1/internal/tenants/{id}/suspend", h.handleSuspendTenant)
	mux.HandleFunc("POST /v1/internal/tenants/{id}/resume", h.handleResumeTenant)
	mux.HandleFunc("POST /v1/internal/tenants/{id}/impersonate", h.handleImpersonate)
	mux.HandleFunc("DELETE /v1/internal/tenants/{id}", h.handleDeleteTenant)
	mux.HandleFunc("GET /v1/internal/providers/health", h.handleProviders)
	mux.HandleFunc("POST /v1/internal/providers/{provider}/circuit-break", h.handlePauseProvider)
	mux.HandleFunc("POST /v1/internal/emergency/provider/{provider}/pause", h.handlePauseProvider)
	mux.HandleFunc("POST /v1/internal/emergency/tenant/{id}/pause", h.handlePauseTenant)
	mux.HandleFunc("POST /v1/internal/emergency/campaign/{id}/pause", h.handlePauseCampaign)
	mux.HandleFunc("GET /v1/internal/jobs/failed", h.handleFailedJobs)
	mux.HandleFunc("GET /v1/internal/support/leads/search", h.handleLeadSearch)
	mux.HandleFunc("GET /v1/internal/audit", h.handleAudit)
	mux.HandleFunc("GET /v1/internal/feature-flags", h.handleFeatureFlags)
	mux.HandleFunc("PUT /v1/internal/feature-flags/{key}", h.handleUpdateFeatureFlag)
	mux.HandleFunc("GET /v1/internal/ai-quality", h.handleQualityQueue)
}

func (h *Handler) handleListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.service.ListTenants(r.Context(), actorFrom(r))
	writeResult(w, tenants, err)
}

func (h *Handler) handleSuspendTenant(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tenant, err := h.service.SuspendTenant(r.Context(), actorFrom(r), r.PathValue("id"), req)
	writeResult(w, tenant, err)
}

func (h *Handler) handleResumeTenant(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tenant, err := h.service.ResumeTenant(r.Context(), actorFrom(r), r.PathValue("id"), req)
	writeResult(w, tenant, err)
}

func (h *Handler) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tenant, err := h.service.DeleteTenant(r.Context(), actorFrom(r), r.PathValue("id"), req)
	writeResult(w, tenant, err)
}

func (h *Handler) handleImpersonate(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token, err := h.service.ImpersonateTenant(r.Context(), actorFrom(r), r.PathValue("id"), req)
	writeResult(w, map[string]string{"token": token}, err)
}

func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.service.ListProviders(r.Context(), actorFrom(r))
	writeResult(w, providers, err)
}

func (h *Handler) handlePauseProvider(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	killSwitch, err := h.service.PauseProvider(r.Context(), actorFrom(r), r.PathValue("provider"), req)
	writeResult(w, killSwitch, err)
}

func (h *Handler) handlePauseTenant(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	killSwitch, err := h.service.PauseScope(r.Context(), actorFrom(r), "tenant", r.PathValue("id"), req)
	writeResult(w, killSwitch, err)
}

func (h *Handler) handlePauseCampaign(w http.ResponseWriter, r *http.Request) {
	var req model.ActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	killSwitch, err := h.service.PauseScope(r.Context(), actorFrom(r), "campaign", r.PathValue("id"), req)
	writeResult(w, killSwitch, err)
}

func (h *Handler) handleFailedJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.service.ListFailedJobs(r.Context(), actorFrom(r))
	writeResult(w, jobs, err)
}

func (h *Handler) handleLeadSearch(w http.ResponseWriter, r *http.Request) {
	results, err := h.service.SearchLeadByPhone(r.Context(), actorFrom(r), r.URL.Query().Get("phone"))
	writeResult(w, results, err)
}

func (h *Handler) handleAudit(w http.ResponseWriter, r *http.Request) {
	filter := model.AuditFilter{
		TenantID: r.URL.Query().Get("tenant_id"),
		ActorID:  r.URL.Query().Get("actor_id"),
		Action:   r.URL.Query().Get("action"),
	}
	entries, err := h.service.ListAudit(r.Context(), actorFrom(r), filter)
	writeResult(w, entries, err)
}

func (h *Handler) handleFeatureFlags(w http.ResponseWriter, r *http.Request) {
	flags, err := h.service.ListFeatureFlags(r.Context(), actorFrom(r))
	writeResult(w, flags, err)
}

func (h *Handler) handleUpdateFeatureFlag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID    string `json:"tenant_id"`
		Enabled     bool   `json:"enabled"`
		Description string `json:"description"`
		Reason      string `json:"reason"`
		TicketID    string `json:"ticket_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	flag := model.FeatureFlag{Key: r.PathValue("key"), TenantID: req.TenantID, Enabled: req.Enabled, Description: req.Description}
	updated, err := h.service.SetFeatureFlag(r.Context(), actorFrom(r), flag, model.ActionRequest{Reason: req.Reason, TicketID: req.TicketID})
	writeResult(w, updated, err)
}

func (h *Handler) handleQualityQueue(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListAIQualityItems(r.Context(), actorFrom(r))
	writeResult(w, items, err)
}

func actorFrom(r *http.Request) model.Actor {
	role := r.Header.Get("X-Actor-Role")
	if role == "" {
		role = r.Header.Get("X-Admin-Role")
	}
	return model.Actor{ID: r.Header.Get("X-Actor-ID"), Role: model.Role(role)}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, service.ErrForbidden):
			status = http.StatusForbidden
		case errors.Is(err, service.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, service.ErrBlocked):
			status = http.StatusLocked
		case errors.Is(err, service.ErrTicketRequired):
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
