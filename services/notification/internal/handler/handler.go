package handler

import (
	"encoding/json"
	"net/http"

	"github.com/lead/services/notification/internal/model"
	"github.com/lead/services/notification/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/notifications/send", h.handleSend)
	mux.HandleFunc("POST /v1/notifications/subscribe", h.handleSubscribe)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) handleSend(w http.ResponseWriter, r *http.Request) {
	var req model.SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	notification, err := h.service.Send(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusAccepted, notification)
		return
	}
	writeJSON(w, http.StatusAccepted, notification)
}

func (h *Handler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req model.Subscription
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sub, err := h.service.Subscribe(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
