package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lead/services/handoff/internal/model"
	"github.com/lead/services/handoff/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/handoffs", h.handleCreateHandoff)
	mux.HandleFunc("POST /v1/handoffs/from-call", h.handleCreateHandoffFromCall)
	mux.HandleFunc("POST /v1/handoffs/", h.handleHandoffAction)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) handleCreateHandoff(w http.ResponseWriter, r *http.Request) {
	var req model.CreateHandoffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	handoff, err := h.service.CreateHandoff(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, handoff)
}

func (h *Handler) handleHandoffAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/handoffs/"), "/")
	if len(parts) != 2 {
		writeErr(w, http.StatusNotFound, "unknown handoff action")
		return
	}
	id := parts[0]
	action := parts[1]
	switch action {
	case "ack":
		var req struct {
			UserID string `json:"user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		handoff, err := h.service.AcknowledgeHandoff(r.Context(), id, req.UserID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, handoff)
	case "escalate":
		handoff, err := h.service.EscalateHandoff(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, handoff)
	case "reassign":
		var req struct {
			NewUserID string `json:"new_user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		handoff, err := h.service.ReassignHandoff(r.Context(), id, req.NewUserID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, handoff)
	default:
		writeErr(w, http.StatusNotFound, "unknown handoff action")
	}
}

// handleCreateHandoffFromCall accepts an inline snapshot so call-intel can
// create a handoff in a single HTTP round-trip without a pre-existing snapshot.
//
// Body: {tenant_id, lead_id, reason, summary, snapshot: {id, tenant_id, lead_id, ...}}
func (h *Handler) handleCreateHandoffFromCall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		model.CreateHandoffRequest
		Snapshot *model.ScoringSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Snapshot != nil {
		// Persist snapshot so CreateHandoff can retrieve it.
		if err := h.service.Store().SaveScoringSnapshot(r.Context(), *body.Snapshot); err != nil {
			writeErr(w, http.StatusInternalServerError, "save snapshot: "+err.Error())
			return
		}
		body.CreateHandoffRequest.ScoringSnapshotID = body.Snapshot.ID
	}
	handoff, err := h.service.CreateHandoff(r.Context(), body.CreateHandoffRequest)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, handoff)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
