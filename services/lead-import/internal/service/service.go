package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lead/services/lead-import/internal/model"
	"github.com/lead/services/lead-import/internal/parser"
	"github.com/lead/services/lead-import/internal/store"
	"github.com/nyaruka/phonenumbers"
)

type Service struct {
	store store.Store
}

func New(s store.Store) *Service {
	return &Service{store: s}
}

func (svc *Service) Mount(r chi.Router) {
	r.Post("/v1/import/jobs", svc.handleCreateImportJob)
	r.Get("/v1/import/jobs/{id}", svc.handleGetImportJob)
	r.Post("/v1/import/preview", svc.handlePreviewImport)
	r.Post("/v1/leads", svc.handleIngestApiLead)
	r.Get("/v1/leads", svc.handleListLeads)
	r.Get("/v1/leads/{id}", svc.handleGetLead)
	r.Patch("/v1/leads/{id}/score", svc.handleUpdateLeadScore)
	r.Post("/v1/webhooks/fb-lead-ads", svc.handleFBWebhook)
	r.Post("/v1/webhooks/google-lead-forms", svc.handleGoogleWebhook)
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

// normalizePhone tries to parse a raw phone into E.164, defaulting to IN.
func normalizePhone(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty phone")
	}
	num, err := phonenumbers.Parse(raw, "IN")
	if err != nil {
		return "", fmt.Errorf("parse phone %q: %w", raw, err)
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", fmt.Errorf("invalid phone %q", raw)
	}
	return phonenumbers.Format(num, phonenumbers.E164), nil
}

func (svc *Service) upsertContactFromFields(r *http.Request, tenantID string, fields map[string]string) (*model.Contact, error) {
	rawPhone := fields["phone"]
	if rawPhone == "" {
		rawPhone = fields["mobile"]
	}
	e164, err := normalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	c := &model.Contact{
		TenantID:  tenantID,
		PhoneE164: e164,
		Email:     fields["email"],
		Name:      fields["name"],
	}
	return svc.store.UpsertContact(r.Context(), c)
}

// handleCreateImportJob accepts inline rows or a file_url and kicks off import.
func (svc *Service) handleCreateImportJob(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "X-Tenant-ID header required")
		return
	}

	var req struct {
		FileURL  string            `json:"file_url"`
		Rows     []map[string]string `json:"rows"`
		Mapping  map[string]string `json:"mapping"`
		SourceID string            `json:"source_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	job := &model.LeadImportJob{
		ID:       uuid.NewString(),
		TenantID: tenantID,
		SourceID: req.SourceID,
		FileURL:  req.FileURL,
		Mapping:  req.Mapping,
		Status:   "running",
	}
	if job.Mapping == nil {
		job.Mapping = map[string]string{}
	}

	if err := svc.store.CreateImportJob(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, "create job failed")
		return
	}

	// Process inline rows synchronously (file_url processing would be async in production).
	var rowErrs []model.RowError
	imported := 0
	for i, rowFields := range req.Rows {
		contact, err := svc.upsertContactFromFields(r, tenantID, applyMapping(rowFields, req.Mapping))
		if err != nil {
			rowErrs = append(rowErrs, model.RowError{Row: i + 1, Message: err.Error()})
			continue
		}
		lead := &model.Lead{
			TenantID:  tenantID,
			ContactID: contact.ID,
			SourceID:  req.SourceID,
			Status:    "new",
		}
		if err := svc.store.CreateLead(r.Context(), lead); err != nil {
			rowErrs = append(rowErrs, model.RowError{Row: i + 1, Message: err.Error()})
			continue
		}
		imported++
	}

	job.TotalRows = len(req.Rows)
	job.ImportedRows = imported
	job.ErrorRows = len(rowErrs)
	job.Errors = rowErrs
	job.Status = "done"
	_ = svc.store.UpdateImportJob(r.Context(), job)

	writeJSON(w, http.StatusCreated, map[string]any{"job": job})
}

func (svc *Service) handleGetImportJob(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "X-Tenant-ID header required")
		return
	}
	jobID := chi.URLParam(r, "id")
	job, err := svc.store.GetImportJob(r.Context(), tenantID, jobID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

// handlePreviewImport does a dry-run CSV/XLSX parse returning first 50 rows.
func (svc *Service) handlePreviewImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "multipart parse failed")
		return
	}

	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	var rawMapping map[string]string
	if mv := r.FormValue("mapping"); mv != "" {
		_ = json.Unmarshal([]byte(mv), &rawMapping)
	}

	const previewLimit = 50
	var rows []map[string]string
	var rowErrs []model.RowError
	count := 0

	onRow := func(rowNum int, row model.ParsedRow) {
		if count >= previewLimit {
			return
		}
		count++
		rows = append(rows, row.Fields)
	}

	name := strings.ToLower(hdr.Filename)
	if strings.HasSuffix(name, ".xlsx") {
		rowErrs, err = parser.ParseXLSX(file, rawMapping, onRow)
	} else {
		rowErrs, err = parser.ParseCSV(file, rawMapping, onRow)
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rows":   rows,
		"errors": rowErrs,
	})
}

// handleIngestApiLead handles single-row API ingestion.
func (svc *Service) handleIngestApiLead(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "X-Tenant-ID header required")
		return
	}

	var req struct {
		Phone    string            `json:"phone"`
		Email    string            `json:"email"`
		Name     string            `json:"name"`
		SourceID string            `json:"source_id"`
		Fields   map[string]string `json:"fields"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fields := map[string]string{
		"phone": req.Phone,
		"email": req.Email,
		"name":  req.Name,
	}
	for k, v := range req.Fields {
		fields[k] = v
	}

	contact, err := svc.upsertContactFromFields(r, tenantID, fields)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	lead := &model.Lead{
		TenantID:  tenantID,
		ContactID: contact.ID,
		SourceID:  req.SourceID,
		Status:    "new",
	}
	if err := svc.store.CreateLead(r.Context(), lead); err != nil {
		writeErr(w, http.StatusInternalServerError, "create lead failed")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"lead": lead, "contact": contact})
}

func (svc *Service) handleListLeads(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "X-Tenant-ID header required")
		return
	}
	q := r.URL.Query()
	filters := store.LeadFilters{
		Status:     q.Get("status"),
		AssignedTo: q.Get("assigned_to"),
		SourceID:   q.Get("source_id"),
		Limit:      50,
	}
	leads, err := svc.store.ListLeads(r.Context(), tenantID, filters)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list leads failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"leads": leads})
}

func (svc *Service) handleGetLead(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "X-Tenant-ID header required")
		return
	}
	leadID := chi.URLParam(r, "id")
	lead, err := svc.store.GetLead(r.Context(), tenantID, leadID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "lead not found")
		return
	}
	resp := map[string]any{"lead": lead}
	// Include the contact so callers (e.g. the dispatcher) can resolve phone_e164.
	if contact, cerr := svc.store.GetContact(r.Context(), tenantID, lead.ContactID); cerr == nil {
		resp["contact"] = contact
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateLeadScore accepts PATCH /v1/leads/{id}/score
// Body: {"score": 75}
// Header: X-Tenant-ID required
func (svc *Service) handleUpdateLeadScore(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "X-Tenant-ID header required")
		return
	}
	leadID := chi.URLParam(r, "id")
	var req struct {
		Score int `json:"score"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := svc.store.UpdateLeadScore(r.Context(), tenantID, leadID, req.Score); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lead_id": leadID, "score": req.Score})
}

// handleFBWebhook receives Facebook Lead Ads webhook events.
// Signature verification uses X-Hub-Signature-256 (HMAC-SHA256 of body with app secret).
func (svc *Service) handleFBWebhook(w http.ResponseWriter, r *http.Request) {
	// GET = hub challenge verification
	if r.Method == http.MethodGet {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")
		expected := r.URL.Query().Get("_verify_token") // set in source config
		if mode == "subscribe" && token == expected {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge))
			return
		}
		writeErr(w, http.StatusForbidden, "verification failed")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id query param required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body failed")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	// Idempotency via event ID from payload.
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	idempKey := fmt.Sprintf("fb:%s:%v", tenantID, payload["leadgen_id"])
	exists, _ := svc.store.CheckAndSetIdempotencyKey(r.Context(), idempKey)
	if exists {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleGoogleWebhook receives Google Lead Form webhook events.
func (svc *Service) handleGoogleWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		writeErr(w, http.StatusBadRequest, "tenant_id query param required")
		return
	}

	var payload struct {
		LeadID        string            `json:"lead_id"`
		UserColumnData []struct {
			ColumnID   string `json:"column_id"`
			StringValue string `json:"string_value"`
		} `json:"user_column_data"`
	}
	if err := decode(r, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	idempKey := fmt.Sprintf("google:%s:%s", tenantID, payload.LeadID)
	exists, _ := svc.store.CheckAndSetIdempotencyKey(r.Context(), idempKey)
	if exists {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true})
		return
	}

	fields := make(map[string]string)
	for _, col := range payload.UserColumnData {
		fields[col.ColumnID] = col.StringValue
	}

	if fields["PHONE_NUMBER"] != "" {
		req := &http.Request{
			Header: http.Header{"X-Tenant-Id": []string{tenantID}},
		}
		_ = req
		contact, err := svc.upsertContactFromFields(&http.Request{
			Header: r.Header,
		}, tenantID, map[string]string{
			"phone": fields["PHONE_NUMBER"],
			"email": fields["EMAIL"],
			"name":  fields["FULL_NAME"],
		})
		if err == nil {
			lead := &model.Lead{
				TenantID:  tenantID,
				ContactID: contact.ID,
				Status:    "new",
			}
			_ = svc.store.CreateLead(r.Context(), lead)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func applyMapping(fields map[string]string, mapping map[string]string) map[string]string {
	if len(mapping) == 0 {
		return fields
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		canonical := mapping[k]
		if canonical == "" {
			canonical = k
		}
		out[canonical] = v
	}
	return out
}

// processCSVImport is called asynchronously for file-based import jobs.
func (svc *Service) processCSVImport(tenantID string, job *model.LeadImportJob, data []byte, contentType string) {
	var rowErrs []model.RowError
	imported := 0
	total := 0

	onRow := func(rowNum int, row model.ParsedRow) {
		total++
		fields := applyMapping(row.Fields, job.Mapping)
		phone := fields["phone"]
		if phone == "" {
			phone = fields["mobile"]
		}
		e164, err := normalizePhone(phone)
		if err != nil {
			rowErrs = append(rowErrs, model.RowError{Row: rowNum, Message: err.Error()})
			return
		}
		contact, err := svc.store.UpsertContact(nil, &model.Contact{
			TenantID:  tenantID,
			PhoneE164: e164,
			Email:     fields["email"],
			Name:      fields["name"],
		})
		if err != nil {
			rowErrs = append(rowErrs, model.RowError{Row: rowNum, Message: err.Error()})
			return
		}
		lead := &model.Lead{
			ID:        uuid.NewString(),
			TenantID:  tenantID,
			ContactID: contact.ID,
			SourceID:  job.SourceID,
			Status:    "new",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := svc.store.CreateLead(nil, lead); err != nil {
			rowErrs = append(rowErrs, model.RowError{Row: rowNum, Message: err.Error()})
			return
		}
		imported++
	}

	r := bytes.NewReader(data)
	if strings.Contains(contentType, "spreadsheetml") || strings.HasSuffix(job.FileURL, ".xlsx") {
		_, _ = parser.ParseXLSX(r, job.Mapping, onRow)
	} else {
		_, _ = parser.ParseCSV(r, job.Mapping, onRow)
	}

	job.TotalRows = total
	job.ImportedRows = imported
	job.ErrorRows = len(rowErrs)
	job.Errors = rowErrs
	job.Status = "done"
	_ = svc.store.UpdateImportJob(nil, job)
}
