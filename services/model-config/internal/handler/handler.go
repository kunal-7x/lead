package handler

import (
	"encoding/json"
	"net/http"

	"github.com/lead/services/model-config/internal/model"
	"github.com/lead/services/model-config/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /v1/admin/models/current", h.handleCurrent)
	mux.HandleFunc("PUT /v1/admin/models/global", h.handleUpdateGlobal)
	mux.HandleFunc("PUT /v1/admin/models/tenant/{id}", h.handleUpdateTenant)
	mux.HandleFunc("DELETE /v1/admin/models/tenant/{id}", h.handleDeleteTenant)
	mux.HandleFunc("GET /v1/admin/models/available", h.handleAvailable)
	mux.HandleFunc("GET /v1/admin/models/health", h.handleHealth)
}

func (h *Handler) handleCurrent(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r) {
		return
	}
	config, err := h.service.Current(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (h *Handler) handleUpdateGlobal(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r) {
		return
	}
	var req model.GlobalSelection
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.service.UpdateGlobal(r.Context(), req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r) {
		return
	}
	var req model.GlobalSelection
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	override, err := h.service.UpdateTenant(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, override)
}

func (h *Handler) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r) {
		return
	}
	if err := h.service.DeleteTenant(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) handleAvailable(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, h.service.Available())
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireSuperAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, h.service.Health(r.Context()))
}

func requireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	role := r.Header.Get("X-Admin-Role")
	if role == "" {
		role = r.Header.Get("X-Actor-Role")
	}
	if role != "super_admin" {
		writeErr(w, http.StatusForbidden, "super_admin role required")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
