package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/consent-compliance/internal/compliance"
	"github.com/lead/services/consent-compliance/internal/model"
	"github.com/lead/services/consent-compliance/internal/pdf"
	"github.com/lead/services/consent-compliance/internal/store"
)

type Handler struct {
	store     store.Store
	windowCfg compliance.WindowConfig
}

func New(s store.Store) *Handler {
	return &Handler{
		store:     s,
		windowCfg: compliance.DefaultWindowConfig(),
	}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/consent/record", h.handleRecordConsent)
	r.Post("/v1/consent/withdraw", h.handleWithdrawConsent)
	r.Post("/v1/consent/check-outreach", h.handleCheckOutreach)
	r.Post("/v1/suppression", h.handleAddSuppression)
	r.Delete("/v1/suppression/{phone}", h.handleRemoveSuppression)
	r.Get("/v1/consent/trail/{lead_id}", h.handleExportConsentTrail)
	r.Post("/v1/consent/erasure", h.handleRequestErasure)
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

func (h *Handler) handleRecordConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeadID        string `json:"lead_id"`
		Basis         string `json:"basis"`
		Source        string `json:"source"`
		NoticeVersion string `json:"notice_version"`
		EvidenceURL   string `json:"evidence_url"`
	}
	if err := decode(r, &req); err != nil || req.LeadID == "" || req.Basis == "" {
		writeErr(w, http.StatusBadRequest, "lead_id and basis are required")
		return
	}
	rec := model.ConsentRecord{
		LeadID:        req.LeadID,
		Basis:         model.ConsentBasis(req.Basis),
		Source:        req.Source,
		NoticeVersion: req.NoticeVersion,
		EvidenceURL:   req.EvidenceURL,
	}
	if err := h.store.RecordConsent(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record consent")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (h *Handler) handleWithdrawConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeadID  string `json:"lead_id"`
		Channel string `json:"channel"`
		Reason  string `json:"reason"`
	}
	if err := decode(r, &req); err != nil || req.LeadID == "" || req.Channel == "" {
		writeErr(w, http.StatusBadRequest, "lead_id and channel are required")
		return
	}
	if err := h.store.WithdrawConsent(r.Context(), req.LeadID, model.Channel(req.Channel), req.Reason); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to withdraw consent")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleCheckOutreach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeadID       string `json:"lead_id"`
		Phone        string `json:"phone"`
		Channel      string `json:"channel"`
		AtTime       string `json:"at_time"`
		CampaignID   string `json:"campaign_id"`
		CampaignType string `json:"campaign_type"`
		CallerID     string `json:"caller_id"`
	}
	if err := decode(r, &req); err != nil || req.LeadID == "" || req.Channel == "" {
		writeErr(w, http.StatusBadRequest, "lead_id and channel are required")
		return
	}

	atTime := time.Now()
	if req.AtTime != "" {
		var err error
		atTime, err = time.Parse(time.RFC3339, req.AtTime)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid at_time (use RFC3339)")
			return
		}
	}

	result := compliance.CheckOutreachAllowed(r.Context(), h.store, compliance.OutreachRequest{
		LeadID:       req.LeadID,
		Phone:        req.Phone,
		Channel:      model.Channel(req.Channel),
		AtTime:       atTime,
		CampaignID:   req.CampaignID,
		CampaignType: req.CampaignType,
		CallerID:     req.CallerID,
		WindowCfg:    h.windowCfg,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"allowed": result.Allowed,
		"reason":  result.Reason,
	})
}

func (h *Handler) handleAddSuppression(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone  string `json:"phone"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil || req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "phone is required")
		return
	}
	if err := h.store.AddSuppression(r.Context(), req.Phone, req.Reason); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add suppression")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (h *Handler) handleRemoveSuppression(w http.ResponseWriter, r *http.Request) {
	phone := chi.URLParam(r, "phone")
	var req struct {
		TicketID string `json:"ticket_id"`
	}
	_ = decode(r, &req)
	if err := h.store.RemoveSuppression(r.Context(), phone, req.TicketID); err != nil {
		writeErr(w, http.StatusNotFound, "phone not in suppression list")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleExportConsentTrail(w http.ResponseWriter, r *http.Request) {
	leadID := chi.URLParam(r, "lead_id")

	records, err := h.store.GetConsentTrail(r.Context(), leadID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to get consent trail")
		return
	}

	if r.Header.Get("Accept") == "application/pdf" {
		pdfBytes, err := pdf.Generate(pdf.ExportData{
			LeadID:      leadID,
			ExportedAt:  time.Now(),
			RecordCount: len(records),
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to generate PDF")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=consent-trail.pdf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdfBytes)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"lead_id":     leadID,
		"records":     records,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) handleRequestErasure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeadID string `json:"lead_id"`
	}
	if err := decode(r, &req); err != nil || req.LeadID == "" {
		writeErr(w, http.StatusBadRequest, "lead_id is required")
		return
	}
	wfID, err := h.store.RequestErasure(r.Context(), req.LeadID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start erasure workflow")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"workflow_id": wfID,
		"lead_id":     req.LeadID,
		"status":      "pending",
	})
}
