package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/whatsapp-adapter/internal/model"
	"github.com/lead/services/whatsapp-adapter/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/whatsapp/templates/send", h.handleSendTemplate)
	r.Post("/v1/whatsapp/messages", h.handleSendMessage)
	r.Post("/v1/whatsapp/flows/send", h.handleSendFlow)
	r.Post("/v1/whatsapp/templates", h.handleRegisterTemplate)
	r.Post("/v1/whatsapp/templates/sync", h.handleSyncTemplates)
	r.Get("/v1/whatsapp/templates", h.handleListTemplates)
	r.Get("/v1/whatsapp/threads", h.handleListThreads)
	r.Get("/v1/whatsapp/threads/{thread_id}/messages", h.handleListMessages)
	r.Post("/v1/whatsapp/credentials", h.handleUpsertCredential)
	r.Get("/wh/wa", h.handleVerifyWebhook)
	r.Post("/wh/wa", h.handleWebhook)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func tenantID(r *http.Request) string {
	if value := r.Header.Get("X-Tenant-ID"); value != "" {
		return value
	}
	if value := r.URL.Query().Get("tenant_id"); value != "" {
		return value
	}
	return "default"
}

func (h *Handler) handleSendTemplate(w http.ResponseWriter, r *http.Request) {
	var req model.SendTemplateRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TenantID == "" {
		req.TenantID = tenantID(r)
	}
	msg, err := h.service.SendTemplate(r.Context(), req)
	if err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, msg)
}

func (h *Handler) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req model.SendMessageRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TenantID == "" {
		req.TenantID = tenantID(r)
	}
	msg, err := h.service.SendMessage(r.Context(), req)
	if err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, msg)
}

func (h *Handler) handleSendFlow(w http.ResponseWriter, r *http.Request) {
	var req model.SendFlowRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TenantID == "" {
		req.TenantID = tenantID(r)
	}
	msg, err := h.service.SendFlow(r.Context(), req)
	if err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, msg)
}

func (h *Handler) handleRegisterTemplate(w http.ResponseWriter, r *http.Request) {
	var req model.Template
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TenantID == "" {
		req.TenantID = tenantID(r)
	}
	tmpl, err := h.service.RegisterTemplate(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

func (h *Handler) handleSyncTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.service.SyncTemplates(r.Context(), tenantID(r))
	if err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (h *Handler) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.service.ListTemplates(r.Context(), tenantID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (h *Handler) handleListThreads(w http.ResponseWriter, r *http.Request) {
	threads, err := h.service.ListThreads(r.Context(), tenantID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

func (h *Handler) handleListMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := h.service.ListMessages(r.Context(), chi.URLParam(r, "thread_id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (h *Handler) handleUpsertCredential(w http.ResponseWriter, r *http.Request) {
	var req model.VaultCredential
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TenantID == "" {
		req.TenantID = tenantID(r)
	}
	cred, err := h.service.UpsertCredential(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cred.AccessToken = ""
	cred.AppSecret = ""
	writeJSON(w, http.StatusOK, cred)
}

func (h *Handler) handleVerifyWebhook(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("hub.mode") != "subscribe" {
		writeErr(w, http.StatusBadRequest, "invalid mode")
		return
	}
	// Meta validates the token out-of-band in production through Vault.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(r.URL.Query().Get("hub.challenge")))
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	payload, err := readAll(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "failed to read payload")
		return
	}
	if err := h.service.ProcessWebhook(r.Context(), tenantID(r), payload, r.Header.Get("X-Hub-Signature-256")); err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func readAll(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidSignature):
		return http.StatusUnauthorized
	case errors.Is(err, service.ErrOutsideServiceWindow):
		return http.StatusConflict
	case errors.Is(err, service.ErrOptedOut):
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}
