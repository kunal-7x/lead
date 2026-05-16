package service

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/lead-identity/internal/phone"
	"github.com/lead/services/lead-identity/internal/store"
)

type Service struct {
	store store.Store
}

func New(s store.Store) *Service {
	return &Service{store: s}
}

func (svc *Service) Mount(r chi.Router) {
	r.Post("/v1/identity/normalize-phone", svc.handleNormalizePhone)
	r.Post("/v1/identity/resolve", svc.handleResolveIdentity)
	r.Post("/v1/identity/merge", svc.handleMergeContacts)
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

func (svc *Service) handleNormalizePhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone         string `json:"phone"`
		DefaultCountry string `json:"default_country"`
	}
	if err := decode(r, &req); err != nil || req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "phone is required")
		return
	}
	country := req.DefaultCountry
	if country == "" {
		country = "IN"
	}
	e164, err := phone.Normalize(req.Phone, country)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"e164": e164})
}

func (svc *Service) handleResolveIdentity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		Phone    string `json:"phone"`
		Email    string `json:"email"`
		Name     string `json:"name"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id and phone are required")
		return
	}

	e164, err := phone.Normalize(req.Phone, "IN")
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	contact, created, err := svc.store.GetOrCreateContact(r.Context(), req.TenantID, e164, req.Email, req.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "resolve identity failed")
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"contact_id": contact.ID,
		"contact":    contact,
		"created":    created,
	})
}

func (svc *Service) handleMergeContacts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID    string `json:"tenant_id"`
		PrimaryID   string `json:"primary_id"`
		DuplicateID string `json:"duplicate_id"`
		ActorID     string `json:"actor_id"`
	}
	if err := decode(r, &req); err != nil || req.TenantID == "" || req.PrimaryID == "" || req.DuplicateID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id, primary_id, and duplicate_id are required")
		return
	}
	if req.PrimaryID == req.DuplicateID {
		writeErr(w, http.StatusBadRequest, "primary_id and duplicate_id must differ")
		return
	}

	if err := svc.store.MergeContacts(r.Context(), req.TenantID, req.PrimaryID, req.DuplicateID, req.ActorID); err != nil {
		writeErr(w, http.StatusInternalServerError, "merge contacts failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "primary_id": req.PrimaryID})
}
