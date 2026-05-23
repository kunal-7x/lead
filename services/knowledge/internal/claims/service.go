package claims

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
)

const (
	ActionBlocked            = "blocked"
	ActionRewritten          = "rewritten"
	ActionAllowedWithWarning = "allowed_with_warning"

	SafeFallback = "Let me check that and get back to you."
)

type Service struct {
	store     store.Store
	publisher events.Publisher
	embedder  embed.Provider
	qdrant    *vector.Client
	now       func() time.Time
}

type CheckRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id"`
	CallID    string `json:"call_id,omitempty"`
	LeadID    string `json:"lead_id,omitempty"`
	Channel   string `json:"channel"`
	Text      string `json:"text"`
}

type Violation struct {
	ClaimID     string  `json:"claim_id,omitempty"`
	ClaimType   string  `json:"claim_type"`
	Reason      string  `json:"reason"`
	ActionTaken string  `json:"action_taken"`
	Score       float32 `json:"score,omitempty"`
}

type CheckResponse struct {
	OK          bool        `json:"ok"`
	ActionTaken string      `json:"action_taken"`
	Violations  []Violation `json:"violations"`
	Rewritten   *string     `json:"rewritten"`
}

func New(s store.Store) *Service {
	return &Service{store: s, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) WithSemantic(e embed.Provider, q *vector.Client) *Service {
	s.embedder = e
	s.qdrant = q
	return s
}

func (s *Service) WithPublisher(p events.Publisher) *Service {
	s.publisher = p
	return s
}

func (s *Service) SaveClaim(ctx context.Context, claim *model.ProjectClaim) error {
	if claim.PatternKind == "" {
		claim.PatternKind = model.ClaimPatternPhrase
	}
	if claim.Status == "" {
		claim.Status = model.ClaimStatusNeedsHumanApproval
	}
	if err := s.store.SaveClaim(ctx, claim); err != nil {
		return err
	}
	if claim.PatternKind == model.ClaimPatternSemantic {
		return s.indexSemanticClaim(ctx, claim)
	}
	return nil
}

func (s *Service) ApproveClaim(ctx context.Context, id, approverID string) (*model.ProjectClaim, error) {
	claim, err := s.store.GetClaim(ctx, id)
	if err != nil {
		return nil, err
	}
	claim.Status = model.ClaimStatusAllowed
	claim.ApproverUserID = approverID
	if err := s.SaveClaim(ctx, claim); err != nil {
		return nil, err
	}
	return claim, nil
}

func (s *Service) Check(ctx context.Context, req CheckRequest) (CheckResponse, error) {
	if req.Channel == "" {
		req.Channel = "voice"
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return CheckResponse{OK: true, ActionTaken: "none"}, nil
	}

	project, _ := s.store.GetProject(ctx, req.ProjectID)
	if req.TenantID == "" && project != nil {
		req.TenantID = project.TenantID
	}

	claims, err := s.activeClaims(ctx, req.ProjectID)
	if err != nil {
		return CheckResponse{}, err
	}

	if v, replacement, ok := s.matchHardBlocks(ctx, req, text, claims, project); ok {
		return s.block(ctx, req, text, v, replacement)
	}

	detected := detectSensitive(text)
	for _, rule := range regulatoryRules(text, project) {
		detected[rule.ClaimType] = rule.Reason
	}
	if len(detected) == 0 {
		return CheckResponse{OK: true, ActionTaken: "none"}, nil
	}

	allowed := allowedClaimTypes(text, claims, s.now)
	for claimType, reason := range detected {
		if !allowed[claimType] {
			return s.block(ctx, req, text, Violation{
				ClaimType:   claimType,
				Reason:      reason,
				ActionTaken: ActionBlocked,
			}, SafeFallback)
		}
	}

	return CheckResponse{OK: true, ActionTaken: "none"}, nil
}

func (s *Service) activeClaims(ctx context.Context, projectID string) ([]*model.ProjectClaim, error) {
	global, err := s.store.ListGlobalClaims(ctx)
	if err != nil {
		return nil, err
	}
	projectClaims, err := s.store.ListClaims(ctx, projectID)
	if err != nil {
		return nil, err
	}
	claims := append(defaultClaims(), global...)
	claims = append(claims, projectClaims...)
	sort.SliceStable(claims, func(i, j int) bool {
		return claims[i].Status > claims[j].Status
	})
	return claims, nil
}

func (s *Service) matchHardBlocks(ctx context.Context, req CheckRequest, text string, claims []*model.ProjectClaim, project *model.Project) (Violation, string, bool) {
	for _, claim := range claims {
		if claim.Status == model.ClaimStatusAllowed || !isActive(claim, s.now) {
			continue
		}
		if claim.PatternKind == model.ClaimPatternSemantic {
			continue
		}
		if matchText(claim, text) {
			return violationFor(claim, "matched "+claim.Status+" claim"), replacementFor(claim), true
		}
	}

	semantic := semanticClaims(claims, s.now)
	if len(semantic) == 0 {
		return Violation{}, "", false
	}
	if s.embedder == nil || s.qdrant == nil {
		claim := semantic[0]
		return violationFor(claim, "semantic claim rules configured but vector backend is unavailable"), replacementFor(claim), true
	}
	vec, err := s.embedder.Embed(ctx, text)
	if err != nil {
		claim := semantic[0]
		return violationFor(claim, "semantic embedding failed: "+err.Error()), replacementFor(claim), true
	}
	hits, err := s.qdrant.Search(ctx, claimCollection(req.TenantID), vec, 5, nil)
	if err != nil {
		claim := semantic[0]
		return violationFor(claim, "semantic claim search failed: "+err.Error()), replacementFor(claim), true
	}
	byID := map[string]*model.ProjectClaim{}
	for _, claim := range semantic {
		byID[claim.ID] = claim
	}
	for _, hit := range hits {
		if hit.Score < 0.85 {
			continue
		}
		claimID, _ := hit.Payload["claim_id"].(string)
		claim := byID[claimID]
		if claim == nil {
			continue
		}
		if claim.ProjectID != "" && claim.ProjectID != req.ProjectID {
			continue
		}
		v := violationFor(claim, "semantic claim match")
		v.Score = hit.Score
		return v, replacementFor(claim), true
	}
	return Violation{}, "", false
}

func (s *Service) block(ctx context.Context, req CheckRequest, text string, v Violation, replacement string) (CheckResponse, error) {
	if replacement == "" {
		replacement = SafeFallback
	}
	action := v.ActionTaken
	if action == "" {
		action = ActionBlocked
	}
	violation := &model.ClaimViolation{
		TenantID:       req.TenantID,
		ProjectID:      req.ProjectID,
		CallID:         req.CallID,
		LeadID:         req.LeadID,
		Channel:        req.Channel,
		AttemptedText:  text,
		MatchedClaimID: v.ClaimID,
		ClaimType:      v.ClaimType,
		ActionTaken:    action,
		Reason:         v.Reason,
		OccurredAt:     s.now(),
	}
	if err := s.store.RecordClaimViolation(ctx, violation); err != nil {
		return CheckResponse{}, err
	}
	if s.publisher != nil {
		payload, err := json.Marshal(violation)
		if err != nil {
			return CheckResponse{}, err
		}
		if err := s.publisher.Publish(ctx, events.SubjectClaimViolation, payload, events.WithIdempotencyKey(violation.ID)); err != nil {
			return CheckResponse{}, err
		}
	}
	return CheckResponse{
		OK:          false,
		ActionTaken: action,
		Violations:  []Violation{v},
		Rewritten:   &replacement,
	}, nil
}

func (s *Service) indexSemanticClaim(ctx context.Context, claim *model.ProjectClaim) error {
	if s.embedder == nil || s.qdrant == nil {
		if claim.Status == model.ClaimStatusAllowed {
			return nil
		}
		return fmt.Errorf("semantic claim %s requires vector backend", claim.ID)
	}
	tenantID := claim.TenantID
	if tenantID == "" && claim.ProjectID != "" {
		if project, err := s.store.GetProject(ctx, claim.ProjectID); err == nil {
			tenantID = project.TenantID
		}
	}
	vec, err := s.embedder.Embed(ctx, claim.Pattern)
	if err != nil {
		return err
	}
	collection := claimCollection(tenantID)
	if err := s.qdrant.EnsureCollection(ctx, collection, s.embedder.Dim()); err != nil {
		return err
	}
	return s.qdrant.Upsert(ctx, collection, []vector.Point{{
		ID:     claim.ID,
		Vector: vec,
		Payload: map[string]any{
			"claim_id":   claim.ID,
			"project_id": claim.ProjectID,
			"tenant_id":  tenantID,
			"status":     claim.Status,
			"claim_type": claim.ClaimType,
			"text":       claim.Pattern,
		},
	}})
}

func defaultClaims() []*model.ProjectClaim {
	return []*model.ProjectClaim{
		def("global-guaranteed-appreciation", "appreciation", `(?i)\b(guaranteed|assured|100%\s*sure|definite)\s+(appreciation|return|roi|profit)\b`, "regex", "Assured returns or guaranteed appreciation cannot be promised."),
		def("global-definite-loan", "loan", `(?i)\b(definite|guaranteed|100%\s*sure)\s+(loan|home loan|bank approval)\b`, "regex", "Loan eligibility must be confirmed by the bank."),
		def("global-fake-urgency", "urgency", `(?i)\b(only\s+today|last\s+chance|book\s+now\s+or\s+lose|price\s+increases\s+tonight)\b`, "regex", "Urgency claims must be approved by the project team."),
		def("harera-gurugram-no-disclaimer", "rera", `(?i)\bnot\s+liable\s+under\s+rera\b|\brera\s+responsibility\s+does\s+not\s+apply\b`, "regex", "Promoters cannot shift RERA responsibility through advertising copy."),
		def("harera-gurugram-exaggeration", "other", `(?i)\b(best\s+guaranteed|risk\s+free|no\s+rera\s+risk|cannot\s+go\s+wrong)\b`, "regex", "HARERA Gurugram requires truthful fact-based advertising without exaggeration."),
	}
}

func def(id, claimType, pattern, kind, reason string) *model.ProjectClaim {
	now := time.Now().UTC()
	return &model.ProjectClaim{
		ID:          id,
		ClaimType:   claimType,
		Status:      model.ClaimStatusForbidden,
		Pattern:     pattern,
		PatternKind: kind,
		Replacement: SafeFallback,
		Source:      reason,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func detectSensitive(text string) map[string]string {
	out := map[string]string{}
	patterns := map[string]string{
		"price":        `(?i)(\b(price|cost|starts?\s+at|from|rs\.?|inr)\b.*\d|\d+(\.\d+)?\s*(cr|crore|lakh|lakhs))`,
		"discount":     `(?i)\b(discount|cashback|waiver|free\s+registration|zero\s+stamp\s+duty)\b`,
		"offer":        `(?i)\b(offer|scheme|deal|subvention|early\s+bird)\b`,
		"possession":   `(?i)\b(possession|handover|delivery)\b`,
		"rera":         `(?i)\b(rera|hrera|harera|rera\s+number|registration\s+number)\b`,
		"loan":         `(?i)\b(loan|emi|bank\s+approval|pre-approved|finance)\b`,
		"roi":          `(?i)\b(roi|return\s+on\s+investment|rental\s+yield)\b`,
		"appreciation": `(?i)\b(appreciation|assured\s+return|guaranteed\s+return|profit)\b`,
		"urgency":      `(?i)\b(only\s+today|last\s+few|limited\s+units|last\s+chance)\b`,
	}
	for claimType, pattern := range patterns {
		if regexp.MustCompile(pattern).MatchString(text) {
			out[claimType] = "unapproved " + claimType + " claim"
		}
	}
	return out
}

type regulatoryRule struct {
	ClaimType string
	Reason    string
}

func regulatoryRules(text string, project *model.Project) []regulatoryRule {
	lower := strings.ToLower(text)
	if !adLike(lower) {
		return nil
	}
	var out []regulatoryRule
	if project == nil || strings.TrimSpace(project.RERANumber) == "" {
		out = append(out, regulatoryRule{"rera", "ad-style copy requires a registered RERA number"})
	}
	if project != nil && isGurugram(project) {
		if !strings.Contains(lower, "haryanarera.gov.in") || !containsRERANumber(lower, project.RERANumber) {
			out = append(out, regulatoryRule{"rera", "Gurugram ad-style copy requires HARERA website and project RERA registration number"})
		}
	}
	return out
}

func adLike(lower string) bool {
	for _, token := range []string{"book", "buy", "launch", "offer", "site visit", "limited", "invest", "available", "apartment", "flat"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func isGurugram(project *model.Project) bool {
	location := strings.ToLower(project.State + " " + project.City + " " + project.Name)
	return strings.Contains(location, "gurugram") || strings.Contains(location, "gurgaon") || strings.Contains(location, "haryana")
}

func containsRERANumber(text, reraNumber string) bool {
	reraNumber = strings.ToLower(strings.TrimSpace(reraNumber))
	if reraNumber == "" {
		return false
	}
	return strings.Contains(text, reraNumber)
}

func allowedClaimTypes(text string, claims []*model.ProjectClaim, now func() time.Time) map[string]bool {
	out := map[string]bool{}
	for _, claim := range claims {
		if claim.Status != model.ClaimStatusAllowed || !isActive(claim, now) {
			continue
		}
		if matchText(claim, text) {
			out[claim.ClaimType] = true
		}
	}
	return out
}

func matchText(claim *model.ProjectClaim, text string) bool {
	switch claim.PatternKind {
	case model.ClaimPatternRegex:
		re, err := regexp.Compile(claim.Pattern)
		return err == nil && re.MatchString(text)
	case model.ClaimPatternPhrase, "":
		return strings.Contains(strings.ToLower(text), strings.ToLower(claim.Pattern))
	default:
		return false
	}
}

func semanticClaims(claims []*model.ProjectClaim, now func() time.Time) []*model.ProjectClaim {
	var out []*model.ProjectClaim
	for _, claim := range claims {
		if claim.PatternKind == model.ClaimPatternSemantic && claim.Status != model.ClaimStatusAllowed && isActive(claim, now) {
			out = append(out, claim)
		}
	}
	return out
}

func isActive(claim *model.ProjectClaim, now func() time.Time) bool {
	t := now()
	if claim.ValidFrom != nil && t.Before(*claim.ValidFrom) {
		return false
	}
	if claim.ValidUntil != nil && t.After(*claim.ValidUntil) {
		return false
	}
	return true
}

func violationFor(claim *model.ProjectClaim, reason string) Violation {
	action := ActionBlocked
	if strings.TrimSpace(claim.Replacement) != "" {
		action = ActionRewritten
	}
	if claim.Status == model.ClaimStatusNeedsHumanApproval {
		reason = "claim needs human approval"
	}
	return Violation{
		ClaimID:     claim.ID,
		ClaimType:   claim.ClaimType,
		Reason:      reason,
		ActionTaken: action,
	}
}

func replacementFor(claim *model.ProjectClaim) string {
	if strings.TrimSpace(claim.Replacement) != "" {
		return claim.Replacement
	}
	return SafeFallback
}

func claimCollection(tenantID string) string {
	if tenantID == "" {
		tenantID = "default"
	}
	clean := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(tenantID))
	return "claim_control_" + clean
}
