// Package dispatcher subscribes to campaign.launched events and drives
// outbound dialling: it seeds a durable dial-queue, then on each 2-second
// tick it places calls via the telephony-adapter and writes Redis session
// context for the voice-agent-worker.
package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ----- Redis writer interface ------------------------------------------------

// RedisWriter is a minimal interface for writing a Redis key with a TTL.
// The production implementation uses the same hand-rolled TCP approach as
// services/telephony-adapter/internal/concurrency/limiter.go.
type RedisWriter interface {
	SetEX(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// ----- Dial-queue store interface -------------------------------------------

// DialRow is one entry in the dispatcher's dial queue.
type DialRow struct {
	ID             string         `json:"id"`
	CampaignID     string         `json:"campaign_id"`
	TenantID       string         `json:"tenant_id"`
	LeadID         string         `json:"lead_id"`
	Phone          string         `json:"phone"`
	ProjectID      string         `json:"project_id"`
	KbVersionID    string         `json:"kb_version_id"`
	Attempts       int            `json:"attempts"`
	Status         string         `json:"status"` // pending | in_flight | done | failed
	NextAttemptAt  time.Time      `json:"next_attempt_at"`
	Disposition    string         `json:"disposition"`
	ProviderCallID string         `json:"provider_call_id,omitempty"`
	CampaignCtx    map[string]any `json:"campaign_ctx,omitempty"`
}

// DialQueueStore persists and retrieves DialRows.
type DialQueueStore interface {
	// SeedRows inserts rows (idempotent by campaign_id+lead_id).
	SeedRows(ctx context.Context, rows []*DialRow) error
	// PendingRows returns all rows for campaignID whose status is "pending"
	// and next_attempt_at <= now.
	PendingRows(ctx context.Context, campaignID string, now time.Time) ([]*DialRow, error)
	// MarkInFlight updates a row to in_flight and stores providerCallID.
	MarkInFlight(ctx context.Context, rowID, providerCallID string) error
	// MarkFailed updates a row to failed.
	MarkFailed(ctx context.Context, rowID string) error
	// ActiveCampaignIDs returns distinct campaign IDs that have pending or in_flight rows.
	ActiveCampaignIDs(ctx context.Context) ([]string, error)
}

// ----- Config ---------------------------------------------------------------

// Config holds all external URLs and identifiers the dispatcher needs.
type Config struct {
	CampaignURL        string
	LeadImportURL      string
	TelephonyURL       string
	FromNumber         string
	PublicWebhookBaseURL string
}

// ----- NATS subscription interface ------------------------------------------

// Subscriber is implemented by *events.JetStream; a no-op is used when NATS
// is unavailable.
type Subscriber interface {
	// SubscribeCampaignLaunched registers a callback for campaign.launched.
	// The callback receives the campaign_id string.
	// Blocks until ctx is done.
	SubscribeCampaignLaunched(ctx context.Context, fn func(campaignID string)) error
}

// ----- Dispatcher -----------------------------------------------------------

// Dispatcher drives the outbound dial loop for campaigns.
type Dispatcher struct {
	sub       Subscriber
	redis     RedisWriter
	client    *http.Client
	queue     DialQueueStore
	ratelimit RateLimiter
	cfg       Config

	mu     sync.Mutex
	active map[string]bool // set of active campaign IDs
}

// New creates a Dispatcher. All dependencies are required; pass a NoopSubscriber
// and NoopRedis when running without infra (dev/test boot).
// rl may be nil — a NoopRateLimiter is used in that case.
func New(sub Subscriber, rw RedisWriter, client *http.Client, q DialQueueStore, cfg Config, opts ...Option) *Dispatcher {
	if client == nil {
		client = http.DefaultClient
	}
	d := &Dispatcher{
		sub:       sub,
		redis:     rw,
		client:    client,
		queue:     q,
		ratelimit: &NoopRateLimiter{},
		cfg:       cfg,
		active:    make(map[string]bool),
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Option is a functional option for Dispatcher.
type Option func(*Dispatcher)

// WithRateLimiter sets the RateLimiter on the Dispatcher.
func WithRateLimiter(rl RateLimiter) Option {
	return func(d *Dispatcher) {
		if rl != nil {
			d.ratelimit = rl
		}
	}
}

// Subscribe registers the NATS consumer. Call once; run in a goroutine.
func (d *Dispatcher) Subscribe(ctx context.Context) {
	if err := d.sub.SubscribeCampaignLaunched(ctx, func(campaignID string) {
		if err := d.onCampaignLaunched(ctx, campaignID); err != nil {
			log.Printf("dispatcher: campaign %s seed error: %v", campaignID, err)
		}
	}); err != nil {
		log.Printf("dispatcher: subscribe error: %v", err)
	}
}

// Run is the ticker loop. Call in a goroutine; honours ctx cancellation.
func (d *Dispatcher) Run(ctx context.Context) {
	// On startup, resume any campaigns that have pending/in_flight rows.
	d.resumeActive(ctx)

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			d.tick(ctx)
		}
	}
}

// resumeActive loads campaigns that still have work and marks them active.
func (d *Dispatcher) resumeActive(ctx context.Context) {
	ids, err := d.queue.ActiveCampaignIDs(ctx)
	if err != nil {
		log.Printf("dispatcher: resumeActive: %v", err)
		return
	}
	d.mu.Lock()
	for _, id := range ids {
		d.active[id] = true
	}
	d.mu.Unlock()
	if len(ids) > 0 {
		log.Printf("dispatcher: resumed %d active campaigns", len(ids))
	}
}

// onCampaignLaunched is called for each campaign.launched event.
func (d *Dispatcher) onCampaignLaunched(ctx context.Context, campaignID string) error {
	campaign, err := d.fetchCampaign(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("fetch campaign: %w", err)
	}
	limits, err := d.fetchLimits(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("fetch limits: %w", err)
	}
	_ = limits // stored on rows; used in Run

	leadIDs, err := d.fetchCampaignLeads(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("fetch leads: %w", err)
	}

	campaignCtxMap := campaignContextToMap(campaign.Context)
	// Store rate-limit caps in the context map so dispatchCampaign can read them.
	campaignCtxMap["_hourly_call_cap"] = float64(limits.HourlyCallCap)
	campaignCtxMap["_daily_call_cap"] = float64(limits.DailyCallCap)
	campaignCtxMap["_cost_cap_inr"] = limits.CostCapINR

	rows := make([]*DialRow, 0, len(leadIDs))
	for _, lid := range leadIDs {
		phone, err := d.resolvePhone(ctx, campaign.TenantID, lid)
		if err != nil {
			log.Printf("dispatcher: campaign %s lead %s no phone (%v), skipping", campaignID, lid, err)
			continue
		}
		rows = append(rows, &DialRow{
			ID:            uuid.New().String(),
			CampaignID:    campaignID,
			TenantID:      campaign.TenantID,
			LeadID:        lid,
			Phone:         phone,
			ProjectID:     campaign.ProjectID,
			KbVersionID:   campaign.KbVersionID,
			Attempts:      0,
			Status:        "pending",
			NextAttemptAt: time.Now(),
			Disposition:   "",
			CampaignCtx:   campaignCtxMap,
		})
	}
	if len(rows) == 0 {
		log.Printf("dispatcher: campaign %s: no leads with valid phones", campaignID)
		return nil
	}
	if err := d.queue.SeedRows(ctx, rows); err != nil {
		return fmt.Errorf("seed rows: %w", err)
	}

	d.mu.Lock()
	d.active[campaignID] = true
	d.mu.Unlock()

	log.Printf("dispatcher: campaign %s seeded %d dial rows", campaignID, len(rows))
	return nil
}

// RunOneTick runs a single dispatch tick. Exported for testing.
func (d *Dispatcher) RunOneTick(ctx context.Context) { d.tick(ctx) }

// tick processes one round of pending rows across all active campaigns.
func (d *Dispatcher) tick(ctx context.Context) {
	d.mu.Lock()
	ids := make([]string, 0, len(d.active))
	for id := range d.active {
		ids = append(ids, id)
	}
	d.mu.Unlock()

	for _, campaignID := range ids {
		d.dispatchCampaign(ctx, campaignID)
	}
}

// dispatchCampaign places calls for all pending rows for one campaign.
func (d *Dispatcher) dispatchCampaign(ctx context.Context, campaignID string) {
	rows, err := d.queue.PendingRows(ctx, campaignID, time.Now())
	if err != nil {
		log.Printf("dispatcher: pending rows for %s: %v", campaignID, err)
		return
	}
	for _, row := range rows {
		// Read rate-limit caps stored in the row's campaign context.
		hourlyCap, dailyCap, costCapINR := extractRateLimitCaps(row.CampaignCtx)

		ok, reason := d.ratelimit.Allowed(ctx, campaignID, hourlyCap, dailyCap, costCapINR)
		if !ok {
			log.Printf("dispatcher: campaign %s rate-limited (%s); skipping tick", campaignID, reason)
			// Leave row pending; it will be retried in a later tick/window.
			return
		}

		placed, done := d.placeCall(ctx, row)
		if done {
			// 503 = no free slot; stop trying for this campaign this tick.
			return
		}
		if placed {
			d.ratelimit.RecordCall(ctx, campaignID)
		}
	}
}

// extractRateLimitCaps reads hourly/daily/cost caps from the row's CampaignCtx.
func extractRateLimitCaps(ctx map[string]any) (hourlyCap, dailyCap int, costCapINR float64) {
	if ctx == nil {
		return 0, 0, 0
	}
	if v, ok := ctx["_hourly_call_cap"].(float64); ok {
		hourlyCap = int(v)
	}
	if v, ok := ctx["_daily_call_cap"].(float64); ok {
		dailyCap = int(v)
	}
	if v, ok := ctx["_cost_cap_inr"].(float64); ok {
		costCapINR = v
	}
	return
}

// placeCall attempts to place a telephony call for a dial row.
// Returns (true, false) on success, (false, true) on 503 (no slot), (false, false) on other errors.
func (d *Dispatcher) placeCall(ctx context.Context, row *DialRow) (placed bool, stop bool) {
	sessionID := uuid.New().String()
	callbackURL := strings.TrimRight(d.cfg.PublicWebhookBaseURL, "/") + "/wh/vobiz/answer"
	maxDuration := 300
	if row.CampaignCtx != nil {
		if v, ok := row.CampaignCtx["max_call_seconds"]; ok {
			switch val := v.(type) {
			case float64:
				if int(val) > 0 {
					maxDuration = int(val)
				}
			case int:
				if val > 0 {
					maxDuration = val
				}
			}
		}
	}

	payload := map[string]any{
		"session_id":        sessionID,
		"tenant_id":         row.TenantID,
		"campaign_id":       row.CampaignID,
		"lead_id":           row.LeadID,
		"project_id":        row.ProjectID,
		"from_number":       d.cfg.FromNumber,
		"to_number":         row.Phone,
		"callback_url":      callbackURL,
		"max_duration":      maxDuration,
		"machine_detection": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("dispatcher: marshal call payload: %v", err)
		return false, false
	}

	url := strings.TrimRight(d.cfg.TelephonyURL, "/") + "/v1/calls"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		log.Printf("dispatcher: new request: %v", err)
		return false, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		log.Printf("dispatcher: telephony POST error: %v", err)
		_ = d.queue.MarkFailed(ctx, row.ID)
		return false, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusServiceUnavailable {
		// No free slot — leave row pending, stop processing this campaign this tick.
		log.Printf("dispatcher: campaign %s: telephony 503 (no slot), will retry next tick", row.CampaignID)
		return false, true
	}

	if resp.StatusCode >= 300 {
		log.Printf("dispatcher: telephony error %d for lead %s: %s", resp.StatusCode, row.LeadID, body)
		_ = d.queue.MarkFailed(ctx, row.ID)
		return false, false
	}

	var out struct {
		ProviderCallID string `json:"provider_call_id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		log.Printf("dispatcher: unmarshal telephony response: %v", err)
		_ = d.queue.MarkFailed(ctx, row.ID)
		return false, false
	}
	providerCallID := out.ProviderCallID
	if providerCallID == "" {
		providerCallID = sessionID
	}

	// Write Redis session context for voice-agent-worker.
	lang := "en-IN"
	if row.CampaignCtx != nil {
		if v, ok := row.CampaignCtx["language"]; ok {
			if s, ok := v.(string); ok && s != "" {
				lang = s
			}
		}
	}
	sessionData := map[string]any{
		"tenant_id":              row.TenantID,
		"campaign_id":            row.CampaignID,
		"kb_version_id":          row.KbVersionID,
		"project_id":             row.ProjectID,
		"lead_id":                row.LeadID,
		"lang":                   lang,
		"system_prompt_version":  "v1",
		"campaign_context":       row.CampaignCtx,
	}
	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		log.Printf("dispatcher: marshal session data: %v", err)
	} else {
		redisKey := fmt.Sprintf("call:session:%s", providerCallID)
		if err := d.redis.SetEX(ctx, redisKey, sessionJSON, 2*time.Hour); err != nil {
			log.Printf("dispatcher: redis SetEX %s: %v", redisKey, err)
		}
	}

	if err := d.queue.MarkInFlight(ctx, row.ID, providerCallID); err != nil {
		log.Printf("dispatcher: mark in_flight row %s: %v", row.ID, err)
	}

	log.Printf("dispatcher: placed call for lead %s, provider_call_id=%s", row.LeadID, providerCallID)
	return true, false
}

// ----- HTTP helpers ---------------------------------------------------------

type campaignResponse struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	ProjectID   string          `json:"project_id"`
	KbVersionID string          `json:"kb_version_id"`
	Context     campaignContext `json:"context"`
}

type campaignContext struct {
	ProductDescription  string              `json:"product_description"`
	Offer               string              `json:"offer"`
	TalkingPoints       []string            `json:"talking_points"`
	ObjectionHandling   []map[string]string `json:"objection_handling"`
	QualifyingQuestions []string            `json:"qualifying_questions"`
	Persona             string              `json:"persona"`
	DoNotSay            []string            `json:"do_not_say"`
	Goal                string              `json:"goal"`
	Language            string              `json:"language"`
	BusinessHours       string              `json:"business_hours"`
}

type campaignLimitsResponse struct {
	MaxCallSeconds int     `json:"max_call_seconds"`
	ConcurrentCap  int     `json:"concurrent_cap"`
	HourlyCallCap  int     `json:"hourly_call_cap"`
	DailyCallCap   int     `json:"daily_call_cap"`
	CostCapINR     float64 `json:"cost_cap_inr"`
}

func (d *Dispatcher) fetchCampaign(ctx context.Context, campaignID string) (*campaignResponse, error) {
	url := strings.TrimRight(d.cfg.CampaignURL, "/") + "/v1/campaigns/" + campaignID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("campaign GET %d: %s", resp.StatusCode, body)
	}
	var c campaignResponse
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *Dispatcher) fetchLimits(ctx context.Context, campaignID string) (*campaignLimitsResponse, error) {
	url := strings.TrimRight(d.cfg.CampaignURL, "/") + "/v1/campaigns/" + campaignID + "/limits"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Limits are optional — return defaults.
		return &campaignLimitsResponse{MaxCallSeconds: 300}, nil
	}
	var l campaignLimitsResponse
	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return &campaignLimitsResponse{MaxCallSeconds: 300}, nil
	}
	return &l, nil
}

func (d *Dispatcher) fetchCampaignLeads(ctx context.Context, campaignID string) ([]string, error) {
	url := strings.TrimRight(d.cfg.CampaignURL, "/") + "/v1/campaigns/" + campaignID + "/leads"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("campaign leads GET %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Leads []struct {
			LeadID string `json:"lead_id"`
		} `json:"leads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Leads))
	for _, l := range out.Leads {
		if l.LeadID != "" {
			ids = append(ids, l.LeadID)
		}
	}
	return ids, nil
}

func (d *Dispatcher) resolvePhone(ctx context.Context, tenantID, leadID string) (string, error) {
	url := strings.TrimRight(d.cfg.LeadImportURL, "/") + "/v1/leads/" + leadID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Tenant-ID", tenantID)
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lead-import GET lead %s: status %d", leadID, resp.StatusCode)
	}
	// Response: {"lead": {..., "phone_e164": "..."}} or nested contact
	var out struct {
		Lead struct {
			PhoneE164 string `json:"phone_e164"`
			Contact   struct {
				PhoneE164 string `json:"phone_e164"`
			} `json:"contact"`
		} `json:"lead"`
		Contact struct {
			PhoneE164 string `json:"phone_e164"`
		} `json:"contact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	phone := out.Lead.PhoneE164
	if phone == "" {
		phone = out.Lead.Contact.PhoneE164
	}
	if phone == "" {
		phone = out.Contact.PhoneE164
	}
	if phone == "" {
		return "", fmt.Errorf("lead %s: no phone_e164 in response", leadID)
	}
	return phone, nil
}

// campaignContextToMap converts a campaignContext to map[string]any for Redis storage.
func campaignContextToMap(c campaignContext) map[string]any {
	raw, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// ----- Production Redis writer ----------------------------------------------

// RedisWriterTCP is a dependency-free Redis SetEX client mirroring the approach
// in services/telephony-adapter/internal/concurrency/limiter.go.
type RedisWriterTCP struct {
	addr string
}

// NewRedisWriterTCP creates a production Redis writer from a redis:// URL.
func NewRedisWriterTCP(redisURL string) *RedisWriterTCP {
	return &RedisWriterTCP{addr: parseRedisAddr(redisURL)}
}

func (r *RedisWriterTCP) SetEX(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", r.addr)
	if err != nil {
		return fmt.Errorf("redis dial: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// SETEX key seconds value
	seconds := int(ttl.Seconds())
	cmd := fmt.Sprintf("*4\r\n$5\r\nSETEX\r\n$%d\r\n%s\r\n$%d\r\n%d\r\n$%d\r\n%s\r\n",
		len(key), key,
		len(fmt.Sprintf("%d", seconds)), seconds,
		len(value), value,
	)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return fmt.Errorf("redis write: %w", err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("redis read: %w", err)
	}
	reply := strings.TrimSpace(string(buf[:n]))
	if reply != "+OK" && !strings.HasPrefix(reply, "+OK") {
		return fmt.Errorf("redis unexpected reply: %s", reply)
	}
	return nil
}

func parseRedisAddr(u string) string {
	u = strings.TrimPrefix(u, "redis://")
	if idx := strings.LastIndex(u, "@"); idx >= 0 {
		u = u[idx+1:]
	}
	if idx := strings.Index(u, "/"); idx >= 0 {
		u = u[:idx]
	}
	if !strings.Contains(u, ":") {
		u = u + ":6379"
	}
	return u
}

// ----- No-op implementations for dev/test without infra --------------------

// NoopSubscriber never delivers messages. Used when NATS_URL is unset.
type NoopSubscriber struct{}

func (n *NoopSubscriber) SubscribeCampaignLaunched(_ context.Context, _ func(string)) error {
	return nil
}

// NoopRedisWriter silently discards writes.
type NoopRedisWriter struct{}

func (n *NoopRedisWriter) SetEX(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

// ----- In-memory DialQueueStore (for tests) ---------------------------------

// FakeDialQueueStore is a thread-safe in-memory DialQueueStore for tests.
type FakeDialQueueStore struct {
	mu   sync.Mutex
	rows map[string]*DialRow // keyed by ID
}

func NewFakeDialQueueStore() *FakeDialQueueStore {
	return &FakeDialQueueStore{rows: make(map[string]*DialRow)}
}

func (f *FakeDialQueueStore) SeedRows(_ context.Context, rows []*DialRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range rows {
		key := r.CampaignID + ":" + r.LeadID
		if _, exists := f.rows[key]; !exists {
			cp := *r
			f.rows[key] = &cp
		}
	}
	return nil
}

func (f *FakeDialQueueStore) PendingRows(_ context.Context, campaignID string, now time.Time) ([]*DialRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*DialRow
	for _, r := range f.rows {
		if r.CampaignID == campaignID && r.Status == "pending" && !r.NextAttemptAt.After(now) {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *FakeDialQueueStore) MarkInFlight(_ context.Context, rowID, providerCallID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == rowID {
			r.Status = "in_flight"
			r.ProviderCallID = providerCallID
			return nil
		}
	}
	return fmt.Errorf("row %s not found", rowID)
}

func (f *FakeDialQueueStore) MarkFailed(_ context.Context, rowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == rowID {
			r.Status = "failed"
			return nil
		}
	}
	return fmt.Errorf("row %s not found", rowID)
}

func (f *FakeDialQueueStore) ActiveCampaignIDs(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := make(map[string]bool)
	for _, r := range f.rows {
		if r.Status == "pending" || r.Status == "in_flight" {
			seen[r.CampaignID] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

// RowByLeadID returns the row for a given lead (for test assertions).
func (f *FakeDialQueueStore) RowByLeadID(campaignID, leadID string) *DialRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[campaignID+":"+leadID]
}

// AllRows returns a snapshot of all rows.
func (f *FakeDialQueueStore) AllRows() []*DialRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*DialRow, 0, len(f.rows))
	for _, r := range f.rows {
		cp := *r
		out = append(out, &cp)
	}
	return out
}
