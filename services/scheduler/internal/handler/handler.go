package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/scheduler/internal/model"
	"github.com/lead/services/scheduler/internal/picker"
)

type Handler struct {
	picker *picker.Picker
}

func New(p *picker.Picker) *Handler {
	return &Handler{picker: p}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/v1/scheduler/pick-next", h.pickNext)
	r.Post("/v1/scheduler/mark-attempt", h.markAttempt)
	r.Get("/v1/scheduler/queue-depth", h.queueDepth)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
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
