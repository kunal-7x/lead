package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/campaign/internal/campaign"
	"github.com/lead/services/campaign/internal/model"
	"github.com/lead/services/campaign/internal/store"
)

type Handler struct {
	store        store.Store
	svc          *campaign.Service
	llmRouterURL string
}

func New(s store.Store) *Handler {
	return &Handler{
		store: s,
		svc:   campaign.New(s),
	}
}

// NewWithLLMRouter creates a Handler with a custom llm-router base URL.
func NewWithLLMRouter(s store.Store, llmRouterURL string) *Handler {
	return &Handler{
		store:        s,
		svc:          campaign.New(s),
		llmRouterURL: llmRouterURL,
	}
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/v1/campaigns", func(r chi.Router) {
		r.Post("/extract", h.extractCampaignContext)
		r.Post("/", h.createCampaign)
		r.Get("/", h.listCampaigns)
		r.Get("/{id}", h.getCampaign)
		r.Post("/{id}/leads", h.attachLeads)
		r.Get("/{id}/leads", h.listLeads)
		r.Post("/{id}/launch", h.launchCampaign)
		r.Post("/{id}/pause", h.pauseCampaign)
		r.Post("/{id}/resume", h.resumeCampaign)
		r.Post("/{id}/archive", h.archiveCampaign)
		r.Get("/{id}/health", h.getCampaignHealth)
		r.Put("/{id}/limits", h.setCampaignLimits)
		r.Get("/{id}/limits", h.getCampaignLimits)
	})

	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func (h *Handler) createCampaign(w http.ResponseWriter, r *http.Request) {
	var c model.Campaign
	if err := decode(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.CreateCampaign(r.Context(), &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *Handler) listCampaigns(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	campaigns, err := h.store.ListCampaigns(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if campaigns == nil {
		campaigns = []*model.Campaign{}
	}
	writeJSON(w, http.StatusOK, campaigns)
}

func (h *Handler) getCampaign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := h.store.GetCampaign(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type attachLeadsBody struct {
	LeadIDs []string `json:"lead_ids"`
}

func (h *Handler) attachLeads(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body attachLeadsBody
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.AttachLeads(r.Context(), id, body.LeadIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"attached": len(body.LeadIDs)})
}

type listLeadsResponse struct {
	CampaignID string                  `json:"campaign_id"`
	Leads      []*model.CampaignLead   `json:"leads"`
}

func (h *Handler) listLeads(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	leads, err := h.svc.ListLeads(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if leads == nil {
		leads = []*model.CampaignLead{}
	}
	writeJSON(w, http.StatusOK, listLeadsResponse{CampaignID: id, Leads: leads})
}

func (h *Handler) launchCampaign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	result, err := h.svc.LaunchCampaign(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type pauseBody struct {
	Reason string `json:"reason"`
}

func (h *Handler) pauseCampaign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body pauseBody
	_ = decode(r, &body)
	if err := h.svc.PauseCampaign(r.Context(), id, body.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": model.StatusPaused})
}

func (h *Handler) resumeCampaign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.ResumeCampaign(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": model.StatusActive})
}

func (h *Handler) archiveCampaign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.ArchiveCampaign(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": model.StatusArchived})
}

func (h *Handler) getCampaignHealth(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	health, err := h.svc.GetCampaignHealth(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (h *Handler) setCampaignLimits(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var limits model.CampaignLimits
	if err := decode(r, &limits); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limits.CampaignID = id
	if err := h.svc.SetCampaignLimits(r.Context(), &limits); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (h *Handler) getCampaignLimits(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limits, err := h.store.GetLimits(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

type extractRequest struct {
	Text string `json:"text"`
}

func (h *Handler) extractCampaignContext(w http.ResponseWriter, r *http.Request) {
	var req extractRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	llmURL := h.llmRouterURL
	if llmURL == "" {
		llmURL = "http://llm-router:8111"
	}
	upstreamURL := fmt.Sprintf("%s/v1/llm/extract", llmURL)

	body, _ := json.Marshal(map[string]string{"text": req.Text})
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build upstream request: "+err.Error())
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "llm-router unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read llm-router response: "+err.Error())
		return
	}
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("llm-router returned %d: %s", resp.StatusCode, string(respBody)))
		return
	}

	var ctx model.CampaignContext
	if err := json.Unmarshal(respBody, &ctx); err != nil {
		writeError(w, http.StatusBadGateway, "invalid response from llm-router: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}
