package handler

import (
	"encoding/json"
	"net/http"

	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/analytics/events", h.handleEvent)
	mux.HandleFunc("GET /v1/reports/{report}", h.handleReport)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) handleEvent(w http.ResponseWriter, r *http.Request) {
	var event model.CanonicalEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	created, err := h.service.Ingest(r.Context(), event)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"created": created})
}

func (h *Handler) handleReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = "default"
	}
	report, err := h.service.Report(r.Context(), tenantID, r.PathValue("report"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
