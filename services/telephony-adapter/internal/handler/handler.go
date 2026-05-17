// Package handler wires the HTTP router for the telephony adapter service.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
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
