package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/kb"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
)

type Handler struct {
	store    store.Store
	kbSvc    *kb.Service
	embedder embed.Provider // nil → /retrieve falls back to legacy precomputed-vector mode
	qdrant   *vector.Client // nil → /retrieve uses in-Postgres cosine scan
}

func New(s store.Store) *Handler {
	return &Handler{
		store: s,
		kbSvc: kb.New(s),
	}
}

// WithVector wires the embedder + Qdrant client into the handler. After this
// /retrieve accepts a `query` text field and performs real vector search.
func (h *Handler) WithVector(e embed.Provider, q *vector.Client) *Handler {
	h.embedder = e
	h.qdrant = q
	h.kbSvc = h.kbSvc.WithIndexer(newIndexerAdapter(h.store, e, q))
	return h
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", h.healthz)
	r.Get("/readyz", h.readyz)

	r.Route("/v1/knowledge", func(r chi.Router) {
		// Projects
		r.Post("/projects", h.createProject)
		r.Get("/projects", h.listProjects)
		r.Get("/projects/{id}", h.getProject)
		r.Put("/projects/{id}", h.updateProject)
		r.Post("/projects/{id}/kb-versions", h.createKbVersion)
		r.Post("/projects/{id}/retrieve", h.retrieveKb)

		// Versions
		r.Post("/versions/{id}/facts", h.addFact)
		r.Post("/versions/{id}/faqs", h.addFAQ)
		r.Post("/versions/{id}/assets", h.addAsset)
		r.Post("/versions/{id}/inventory", h.addInventory)
		r.Post("/versions/{id}/offers", h.addOffer)
		r.Post("/versions/{id}/disclaimers", h.addDisclaimer)
		r.Post("/versions/{id}/submit", h.submitForApproval)
		r.Post("/versions/{id}/approve", h.approveVersion)
		r.Post("/versions/{id}/reject", h.rejectVersion)
		r.Post("/versions/{id}/publish", h.publishVersion)

		// Pronunciations
		r.Post("/pronunciations", h.addPronunciation)
		r.Get("/pronunciations", h.listPronunciations)
	})

	return r
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz also probes the embedding backend and Qdrant. Real ping, not just
// a constant — so a misconfigured deploy is caught at boot, not first query.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{"status": "ok"}
	if h.embedder != nil {
		body["embedder"] = h.embedder.Name()
		body["embedding_dim"] = h.embedder.Dim()
	}
	if h.qdrant != nil {
		if err := h.qdrant.Healthy(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "qdrant_unavailable",
				"error":  err.Error(),
			})
			return
		}
		body["qdrant"] = "ok"
	}
	writeJSON(w, http.StatusOK, body)
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

// Projects

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var p model.Project
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.CreateProject(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var p model.Project
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = id
	if err := h.store.UpdateProject(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.store.GetProject(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	projects, err := h.store.ListProjects(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if projects == nil {
		projects = []*model.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

// KB Versions

func (h *Handler) createKbVersion(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var v model.KbVersion
	if err := decode(r, &v); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v.ProjectID = projectID
	if v.Status == "" {
		v.Status = model.VersionDraft
	}
	if err := h.store.CreateKbVersion(r.Context(), &v); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// Content endpoints

func (h *Handler) addFact(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var f model.Fact
	if err := decode(r, &f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f.VersionID = versionID
	if err := h.store.AddFact(r.Context(), &f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (h *Handler) addFAQ(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var f model.FAQ
	if err := decode(r, &f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f.VersionID = versionID
	if err := h.store.AddFAQ(r.Context(), &f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (h *Handler) addAsset(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var a model.Asset
	if err := decode(r, &a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.VersionID = versionID
	if err := h.store.AddAsset(r.Context(), &a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) addInventory(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var inv model.Inventory
	if err := decode(r, &inv); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inv.VersionID = versionID
	if err := h.store.AddInventory(r.Context(), &inv); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (h *Handler) addOffer(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var o model.Offer
	if err := decode(r, &o); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	o.VersionID = versionID
	if err := h.store.AddOffer(r.Context(), &o); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (h *Handler) addDisclaimer(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var d model.Disclaimer
	if err := decode(r, &d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.VersionID = versionID
	if err := h.store.AddDisclaimer(r.Context(), &d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// Approval workflow

type actorBody struct {
	ActorID string `json:"actor_id"`
}

type reviewBody struct {
	ReviewerID string `json:"reviewer_id"`
	Notes      string `json:"notes"`
}

func (h *Handler) submitForApproval(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var body actorBody
	_ = decode(r, &body)
	if err := h.kbSvc.SubmitForApproval(r.Context(), versionID, body.ActorID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "pending_approval"})
}

func (h *Handler) approveVersion(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var body reviewBody
	_ = decode(r, &body)
	if err := h.kbSvc.Approve(r.Context(), versionID, body.ReviewerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handler) rejectVersion(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var body reviewBody
	_ = decode(r, &body)
	if err := h.kbSvc.Reject(r.Context(), versionID, body.ReviewerID, body.Notes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (h *Handler) publishVersion(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "id")
	var body actorBody
	_ = decode(r, &body)
	if err := h.kbSvc.Publish(r.Context(), versionID, body.ActorID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// Retrieval

type retrieveRequest struct {
	Query          string    `json:"query"`           // preferred: raw text, server-side embed
	QueryEmbedding []float32 `json:"query_embedding"` // legacy: pre-computed vector
	TopK           int       `json:"top_k"`
}

func (h *Handler) retrieveKb(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var req retrieveRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.TopK <= 0 {
		req.TopK = 8
	}

	// Path A — vector backend wired AND a real query text supplied → Qdrant.
	if h.embedder != nil && h.qdrant != nil && req.Query != "" {
		result, err := h.retrieveViaQdrant(r, projectID, req.Query, req.TopK)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Path B — caller already has an embedding (legacy path / internal callers).
	result, err := h.store.RetrieveKb(r.Context(), projectID, req.QueryEmbedding, req.TopK)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) retrieveViaQdrant(r *http.Request, projectID, query string, topK int) (*model.RetrieveResult, error) {
	ctx := r.Context()
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.ActiveVersionID == "" {
		return &model.RetrieveResult{}, nil
	}
	vec, err := h.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	collection := vector.CollectionFor(project.TenantID)
	filter := map[string]any{
		"must": []map[string]any{
			vector.MatchKeyword("project_id", project.ID),
			vector.MatchKeyword("kb_version_id", project.ActiveVersionID),
		},
	}
	hits, err := h.qdrant.Search(ctx, collection, vec, topK, filter)
	if err != nil {
		return nil, err
	}
	chunks := make([]model.RetrievedChunk, 0, len(hits))
	for _, hit := range hits {
		payload := hit.Payload
		text, _ := payload["text"].(string)
		srcType, _ := payload["source_type"].(string)
		chunks = append(chunks, model.RetrievedChunk{
			VersionID: project.ActiveVersionID,
			ChunkType: srcType,
			Content:   text,
			Score:     hit.Score,
		})
	}
	return &model.RetrieveResult{VersionStamp: project.ActiveVersionID, Chunks: chunks}, nil
}

// Pronunciations

func (h *Handler) addPronunciation(w http.ResponseWriter, r *http.Request) {
	var p model.Pronunciation
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.Lang == "" {
		p.Lang = "en"
	}
	if err := h.store.AddPronunciation(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) listPronunciations(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	lang := r.URL.Query().Get("lang")
	pronunciations, err := h.store.ListPronunciations(r.Context(), tenantID, lang)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pronunciations == nil {
		pronunciations = []*model.Pronunciation{}
	}
	writeJSON(w, http.StatusOK, pronunciations)
}
