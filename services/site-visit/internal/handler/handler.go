package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/lead/services/site-visit/internal/model"
	"github.com/lead/services/site-visit/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/site-visits", h.handleCreateTentative)
	mux.HandleFunc("GET /v1/site-visits", h.handleListVisits)
	mux.HandleFunc("POST /v1/site-visits/", h.handleAction)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) handleCreateTentative(w http.ResponseWriter, r *http.Request) {
	var req model.CreateTentativeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	visit, err := h.service.CreateTentative(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, visit)
}

func (h *Handler) handleListVisits(w http.ResponseWriter, r *http.Request) {
	visits, err := h.service.ListVisits(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site_visits": visits})
}

func (h *Handler) handleAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/site-visits/"), "/")
	if len(parts) != 2 {
		writeErr(w, http.StatusNotFound, "unknown action")
		return
	}
	id := parts[0]
	switch parts[1] {
	case "confirm":
		var req struct {
			Slot       model.ProposedSlot `json:"slot"`
			SalesRepID string             `json:"sales_rep_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		visit, err := h.service.Confirm(r.Context(), id, req.Slot, req.SalesRepID)
		writeVisit(w, visit, err)
	case "reschedule":
		var req struct {
			NewSlot model.ProposedSlot `json:"new_slot"`
			Reason  string             `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		visit, err := h.service.Reschedule(r.Context(), id, req.NewSlot, req.Reason)
		writeVisit(w, visit, err)
	case "complete":
		var req struct {
			Notes         string `json:"notes"`
			AttendeeCount int    `json:"attendee_count"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		visit, err := h.service.MarkCompleted(r.Context(), id, req.Notes, req.AttendeeCount)
		writeVisit(w, visit, err)
	case "no-show":
		visit, err := h.service.MarkNoShow(r.Context(), id)
		writeVisit(w, visit, err)
	case "lost":
		var req struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		visit, err := h.service.MarkLost(r.Context(), id, req.Reason)
		writeVisit(w, visit, err)
	case "workflows":
		var req struct {
			Now time.Time `json:"now"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Now.IsZero() {
			req.Now = time.Now().UTC()
		}
		actions, err := h.service.EvaluateWorkflows(r.Context(), req.Now)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"fired": actions})
	default:
		writeErr(w, http.StatusNotFound, "unknown action")
	}
}

func writeVisit(w http.ResponseWriter, visit model.Visit, err error) {
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, visit)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
