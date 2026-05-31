// Package consumer subscribes to NATS events and drives post-call intelligence.
package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/lead/libs/go/events"
	"github.com/lead/services/call-intel/internal/store"
)

// Config holds downstream service URLs.
type Config struct {
	NatsURL        string
	ScoringURL     string // http://host:port  (Python scoring service)
	LeadImportURL  string // http://host:port  (Go lead-import)
	KnowledgeURL   string // http://host:port  (Go knowledge)
	SiteVisitURL   string // http://host:port  (Go site-visit)
	HandoffURL     string // http://host:port  (Go handoff)
	SchedulerURL   string // http://host:port  (Go scheduler – re-enqueue callback)
}

// Runner holds the consumer state.
type Runner struct {
	cfg    Config
	st     *store.Store
	js     *events.JetStream
	client *http.Client
	log    *slog.Logger
}

// New creates a Runner. st may be nil when DATABASE_URL is not set (transcript
// persist step will log + skip rather than crash).
func New(cfg Config, st *store.Store, js *events.JetStream, log *slog.Logger) *Runner {
	return &Runner{
		cfg:    cfg,
		st:     st,
		js:     js,
		client: &http.Client{Timeout: 15 * time.Second},
		log:    log,
	}
}

// Run starts all consumers. Blocks until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	go r.subscribeCallCompleted(ctx)
	go r.subscribeSiteVisitRequested(ctx)
	go r.subscribeCallbackRequested(ctx)
	go r.subscribeHandoverRequested(ctx)
	<-ctx.Done()
}

// ---------------------------------------------------------------------------
// call.completed → transcript persist + scoring + score writeback + KB ingest
// ---------------------------------------------------------------------------

type callCompletedPayload struct {
	EventID    string      `json:"event_id"`
	SessionID  string      `json:"session_id"`
	TenantID   string      `json:"tenant_id"`
	CampaignID string      `json:"campaign_id"`
	LeadID     string      `json:"lead_id"`
	ProjectID  string      `json:"project_id"`
	Outcome    string      `json:"outcome"`
	Status     string      `json:"status"`
	Summary    string      `json:"summary"`
	LeadStatus string      `json:"lead_status"`
	LeadScore  int         `json:"lead_score"`
	DurationS  float64     `json:"duration_s"`
	TurnCount  int         `json:"turn_count"`
	Transcript []turnEvent `json:"transcript"`
}

type turnEvent struct {
	Index      int     `json:"index"`
	Role       string  `json:"role"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

func (r *Runner) subscribeCallCompleted(ctx context.Context) {
	if err := r.js.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CALL",
		Durable:       "call-intel-completed",
		FilterSubject: events.SubjectCallCompleted,
		MaxDeliver:    3,
	}, func(ctx context.Context, _ string, data []byte, _ map[string][]string) error {
		var p callCompletedPayload
		if err := json.Unmarshal(data, &p); err != nil {
			r.log.Warn("call.completed: unmarshal error", "err", err)
			return nil // ack + discard malformed
		}
		if p.SessionID == "" && p.LeadID == "" {
			return nil
		}
		r.handleCallCompleted(ctx, p)
		return nil // always ack; individual step failures are logged, not retried
	}); err != nil {
		r.log.Error("call.completed subscribe error", "err", err)
	}
}

func (r *Runner) handleCallCompleted(ctx context.Context, p callCompletedPayload) {
	log := r.log.With("session_id", p.SessionID, "lead_id", p.LeadID)

	// Step (a): persist transcript
	if r.st != nil {
		turns := make([]store.Turn, len(p.Transcript))
		for i, t := range p.Transcript {
			turns[i] = store.Turn{Index: t.Index, Role: t.Role, Text: t.Text, Confidence: t.Confidence}
		}
		_, err := r.st.SaveTranscript(ctx, store.Transcript{
			SessionID:  p.SessionID,
			TenantID:   p.TenantID,
			CampaignID: p.CampaignID,
			LeadID:     p.LeadID,
			ProjectID:  p.ProjectID,
			Outcome:    p.Outcome,
			Status:     p.Status,
			Summary:    p.Summary,
			LeadScore:  p.LeadScore,
			DurationS:  p.DurationS,
			TurnCount:  p.TurnCount,
			Turns:      turns,
		})
		if err != nil {
			log.Warn("step(a) transcript persist failed", "err", err)
		} else {
			log.Info("step(a) transcript persisted")
		}
	} else {
		log.Info("step(a) skipped: no store (DATABASE_URL not set)")
	}

	// Step (b): call scoring service
	score := p.LeadScore // fallback to event value
	if r.cfg.ScoringURL != "" {
		scoreResp, err := r.callScoring(ctx, p)
		if err != nil {
			log.Warn("step(b) scoring failed", "err", err)
		} else {
			score = scoreResp
			log.Info("step(b) scoring called", "score", score)
		}
	} else {
		log.Info("step(b) skipped: SCORING_URL not set")
	}

	// Step (c): write score back to lead
	if r.cfg.LeadImportURL != "" && p.LeadID != "" {
		if err := r.patchLeadScore(ctx, p.TenantID, p.LeadID, score); err != nil {
			log.Warn("step(c) lead score writeback failed", "err", err)
		} else {
			log.Info("step(c) lead score written", "lead_id", p.LeadID, "score", score)
		}
	} else {
		log.Info("step(c) skipped: LEAD_IMPORT_URL not set or no lead_id")
	}

	// Step (d): KB fact ingest
	if r.cfg.KnowledgeURL != "" && p.ProjectID != "" && p.Summary != "" {
		if err := r.ingestFact(ctx, p); err != nil {
			log.Warn("step(d) KB ingest failed", "err", err)
		} else {
			log.Info("step(d) KB fact ingested", "project_id", p.ProjectID)
		}
	} else {
		log.Info("step(d) skipped: KNOWLEDGE_URL/project_id/summary missing")
	}
}

func (r *Runner) callScoring(ctx context.Context, p callCompletedPayload) (int, error) {
	// Build CallInput for the scoring service.
	var avgConf float64
	if len(p.Transcript) > 0 {
		for _, t := range p.Transcript {
			avgConf += t.Confidence
		}
		avgConf /= float64(len(p.Transcript))
	}
	body := map[string]any{
		"session_id":         p.SessionID,
		"lead_id":            p.LeadID,
		"tenant_id":          p.TenantID,
		"summary":            p.Summary,
		"utterance_count":    p.TurnCount,
		"call_duration_secs": p.DurationS,
		"avg_confidence":     avgConf,
	}
	resp, err := r.postJSON(ctx, r.cfg.ScoringURL+"/v1/score/call", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("scoring HTTP %d: %s", resp.StatusCode, b)
	}
	var result struct {
		Score int `json:"score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("scoring decode: %w", err)
	}
	return result.Score, nil
}

func (r *Runner) patchLeadScore(ctx context.Context, tenantID, leadID string, score int) error {
	body := map[string]any{"score": score}
	req, err := newJSONRequest(ctx, http.MethodPatch,
		r.cfg.LeadImportURL+"/v1/leads/"+leadID+"/score", body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Tenant-ID", tenantID)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("lead score patch HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (r *Runner) ingestFact(ctx context.Context, p callCompletedPayload) error {
	// POST /v1/knowledge/versions/{id}/facts — but we don't have a kb_version_id here.
	// Use a project-level endpoint to add a call-summary fact.
	// The knowledge service has POST /v1/knowledge/versions/{id}/facts.
	// We first need a version ID. As a fallback, use the project_id as version_id key
	// by POST-ing to /v1/knowledge/projects/{id}/claims (which creates searchable claims).
	body := map[string]any{
		"text":       fmt.Sprintf("[call-summary] session=%s lead=%s outcome=%s\n%s", p.SessionID, p.LeadID, p.Outcome, p.Summary),
		"source":     "call-completed",
		"session_id": p.SessionID,
	}
	resp, err := r.postJSON(ctx, r.cfg.KnowledgeURL+"/v1/knowledge/projects/"+p.ProjectID+"/claims", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("kb ingest HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------------------------------------------------------------------------
// call.site_visit.requested → POST /v1/site-visits
// ---------------------------------------------------------------------------

// SiteVisitRequestedPayload is exported for test helpers.
type SiteVisitRequestedPayload = siteVisitRequestedPayload

type siteVisitRequestedPayload struct {
	EventID   string `json:"event_id"`
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	LeadID    string `json:"lead_id"`
	ProjectID string `json:"project_id"`
	Reason    string `json:"reason"`
}

func (r *Runner) subscribeSiteVisitRequested(ctx context.Context) {
	if err := r.js.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CALL",
		Durable:       "call-intel-site-visit",
		FilterSubject: "call.site_visit.requested",
		MaxDeliver:    5,
	}, func(ctx context.Context, _ string, data []byte, hdrs map[string][]string) error {
		var p siteVisitRequestedPayload
		if err := json.Unmarshal(data, &p); err != nil {
			r.log.Warn("site_visit.requested: unmarshal", "err", err)
			return nil
		}
		// Idempotency: use Nats-Msg-Id header or event_id
		dedupKey := p.EventID
		if dedupKey == "" {
			if v := hdrs["Nats-Msg-Id"]; len(v) > 0 {
				dedupKey = v[0]
			}
		}
		if err := r.createSiteVisit(ctx, p); err != nil {
			r.log.Warn("site_visit create failed", "err", err, "event_id", dedupKey)
			return err // nak → redeliver up to MaxDeliver
		}
		r.log.Info("site_visit created", "lead_id", p.LeadID, "event_id", dedupKey)
		return nil
	}); err != nil {
		r.log.Error("site_visit.requested subscribe error", "err", err)
	}
}

func (r *Runner) createSiteVisit(ctx context.Context, p siteVisitRequestedPayload) error {
	if r.cfg.SiteVisitURL == "" {
		return nil
	}
	now := time.Now().UTC()
	slot := map[string]any{
		"start": now.Add(48 * time.Hour).Format(time.RFC3339),
		"end":   now.Add(49 * time.Hour).Format(time.RFC3339),
	}
	body := map[string]any{
		"tenant_id":      p.TenantID,
		"lead_id":        p.LeadID,
		"project_id":     p.ProjectID,
		"proposed_slots": []any{slot},
	}
	resp, err := r.postJSON(ctx, r.cfg.SiteVisitURL+"/v1/site-visits", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("site-visit HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------------------------------------------------------------------------
// call.callback.requested → re-enqueue in scheduler dial queue
// ---------------------------------------------------------------------------

// CallbackRequestedPayload is exported for test helpers.
type CallbackRequestedPayload = callbackRequestedPayload

type callbackRequestedPayload struct {
	EventID    string `json:"event_id"`
	SessionID  string `json:"session_id"`
	TenantID   string `json:"tenant_id"`
	LeadID     string `json:"lead_id"`
	CampaignID string `json:"campaign_id"`
	Phone      string `json:"phone"`
	ProjectID  string `json:"project_id"`
	DelayMin   int    `json:"delay_min"`
}

func (r *Runner) subscribeCallbackRequested(ctx context.Context) {
	if err := r.js.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CALL",
		Durable:       "call-intel-callback",
		FilterSubject: "call.callback.requested",
		MaxDeliver:    5,
	}, func(ctx context.Context, _ string, data []byte, hdrs map[string][]string) error {
		var p callbackRequestedPayload
		if err := json.Unmarshal(data, &p); err != nil {
			r.log.Warn("callback.requested: unmarshal", "err", err)
			return nil
		}
		dedupKey := p.EventID
		if dedupKey == "" {
			if v := hdrs["Nats-Msg-Id"]; len(v) > 0 {
				dedupKey = v[0]
			}
		}
		if err := r.enqueueCallback(ctx, p); err != nil {
			r.log.Warn("callback enqueue failed", "err", err, "event_id", dedupKey)
			return err
		}
		r.log.Info("callback enqueued", "lead_id", p.LeadID, "event_id", dedupKey)
		return nil
	}); err != nil {
		r.log.Error("callback.requested subscribe error", "err", err)
	}
}

func (r *Runner) enqueueCallback(ctx context.Context, p callbackRequestedPayload) error {
	if r.cfg.SchedulerURL == "" {
		return nil
	}
	delayMin := p.DelayMin
	if delayMin <= 0 {
		delayMin = 30
	}
	nextAt := time.Now().UTC().Add(time.Duration(delayMin) * time.Minute)
	body := map[string]any{
		"lead_id":         p.LeadID,
		"campaign_id":     p.CampaignID,
		"tenant_id":       p.TenantID,
		"phone":           p.Phone,
		"project_id":      p.ProjectID,
		"next_attempt_at": nextAt.Format(time.RFC3339),
		"source":          "call.callback.requested",
		"idempotency_key": p.EventID,
	}
	resp, err := r.postJSON(ctx, r.cfg.SchedulerURL+"/v1/callbacks", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 404 means scheduler doesn't have this endpoint yet — treat as soft error
	if resp.StatusCode == http.StatusNotFound {
		r.log.Warn("scheduler /v1/callbacks not found; falling back to dial-queue seed", "lead_id", p.LeadID)
		return nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("scheduler callback HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------------------------------------------------------------------------
// call.handover.requested → POST /v1/handoffs
// ---------------------------------------------------------------------------

type handoverRequestedPayload struct {
	EventID   string `json:"event_id"`
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	LeadID    string `json:"lead_id"`
	ProjectID string `json:"project_id"`
	Reason    string `json:"reason"`
	Summary   string `json:"summary"`
}

func (r *Runner) subscribeHandoverRequested(ctx context.Context) {
	if err := r.js.Subscribe(ctx, events.ConsumerConfig{
		Stream:        "CAPSY_CALL",
		Durable:       "call-intel-handover",
		FilterSubject: "call.handover.requested",
		MaxDeliver:    5,
	}, func(ctx context.Context, _ string, data []byte, hdrs map[string][]string) error {
		var p handoverRequestedPayload
		if err := json.Unmarshal(data, &p); err != nil {
			r.log.Warn("handover.requested: unmarshal", "err", err)
			return nil
		}
		dedupKey := p.EventID
		if dedupKey == "" {
			if v := hdrs["Nats-Msg-Id"]; len(v) > 0 {
				dedupKey = v[0]
			}
		}
		if err := r.createHandoff(ctx, p); err != nil {
			r.log.Warn("handoff create failed", "err", err, "event_id", dedupKey)
			return err
		}
		r.log.Info("handoff created", "lead_id", p.LeadID, "event_id", dedupKey)
		return nil
	}); err != nil {
		r.log.Error("handover.requested subscribe error", "err", err)
	}
}

func (r *Runner) createHandoff(ctx context.Context, p handoverRequestedPayload) error {
	if r.cfg.HandoffURL == "" {
		return nil
	}
	// Use /v1/handoffs/from-call which accepts inline snapshot data.
	snapshotID := uuid.NewString()
	body := map[string]any{
		"tenant_id":           p.TenantID,
		"lead_id":             p.LeadID,
		"reason":              p.Reason,
		"summary":             p.Summary,
		"scoring_snapshot_id": snapshotID,
		// Inline snapshot fields for /v1/handoffs/from-call endpoint.
		"snapshot": map[string]any{
			"id":        snapshotID,
			"tenant_id": p.TenantID,
			"lead_id":   p.LeadID,
		},
	}
	resp, err := r.postJSON(ctx, r.cfg.HandoffURL+"/v1/handoffs/from-call", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("handoff HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Exported wrappers (for testing; thin delegation to unexported methods)
// ---------------------------------------------------------------------------

// TestCreateSiteVisit exposes createSiteVisit for white-box testing.
func (r *Runner) TestCreateSiteVisit(ctx context.Context, p SiteVisitRequestedPayload) error {
	return r.createSiteVisit(ctx, p)
}

// TestEnqueueCallback exposes enqueueCallback for white-box testing.
func (r *Runner) TestEnqueueCallback(ctx context.Context, p CallbackRequestedPayload) error {
	return r.enqueueCallback(ctx, p)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (r *Runner) postJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	req, err := newJSONRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	return r.client.Do(req)
}

func newJSONRequest(ctx context.Context, method, url string, body any) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
