package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/knowledge/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_knowledge")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) CreateProject(ctx context.Context, project *model.Project) error {
	now := time.Now().UTC()
	if project.ID == "" {
		project.ID = pgkv.NewID("")
	}
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now
	return p.kv.Put(ctx, "projects", project.ID, *project)
}

func (p *PostgresStore) UpdateProject(ctx context.Context, project *model.Project) error {
	if _, err := p.GetProject(ctx, project.ID); err != nil {
		return err
	}
	project.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "projects", project.ID, *project)
}

func (p *PostgresStore) GetProject(ctx context.Context, id string) (*model.Project, error) {
	project, ok, err := pgkv.Get[model.Project](ctx, p.kv, "projects", id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("project not found")
	}
	return &project, nil
}

func (p *PostgresStore) ListProjects(ctx context.Context, tenantID string) ([]*model.Project, error) {
	projects, err := pgkv.List[model.Project](ctx, p.kv, "projects")
	if err != nil {
		return nil, err
	}
	out := make([]*model.Project, 0, len(projects))
	for _, project := range projects {
		if project.TenantID == tenantID {
			cp := project
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (p *PostgresStore) CreateKbVersion(ctx context.Context, version *model.KbVersion) error {
	versions, err := p.ListKbVersions(ctx, version.ProjectID)
	if err != nil {
		return err
	}
	maxVersion := 0
	for _, existing := range versions {
		if existing.VersionNumber > maxVersion {
			maxVersion = existing.VersionNumber
		}
	}
	now := time.Now().UTC()
	if version.ID == "" {
		version.ID = pgkv.NewID("")
	}
	if version.EmbeddingModel == "" {
		version.EmbeddingModel = "BAAI/bge-m3"
	}
	version.VersionNumber = maxVersion + 1
	if version.CreatedAt.IsZero() {
		version.CreatedAt = now
	}
	version.UpdatedAt = now
	return p.kv.Put(ctx, "kb_versions", version.ID, *version)
}

func (p *PostgresStore) GetKbVersion(ctx context.Context, id string) (*model.KbVersion, error) {
	version, ok, err := pgkv.Get[model.KbVersion](ctx, p.kv, "kb_versions", id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("version not found")
	}
	return &version, nil
}

func (p *PostgresStore) UpdateKbVersionStatus(ctx context.Context, id, status, reviewedBy, notes string) error {
	version, err := p.GetKbVersion(ctx, id)
	if err != nil {
		return err
	}
	version.Status = status
	if reviewedBy != "" {
		version.ReviewedBy = reviewedBy
	}
	if notes != "" {
		version.ReviewNotes = notes
	}
	version.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "kb_versions", id, *version)
}

func (p *PostgresStore) SetActiveVersion(ctx context.Context, projectID, versionID string) error {
	project, err := p.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	project.ActiveVersionID = versionID
	project.UpdatedAt = time.Now().UTC()
	return p.kv.Put(ctx, "projects", projectID, *project)
}

func (p *PostgresStore) ListKbVersions(ctx context.Context, projectID string) ([]*model.KbVersion, error) {
	versions, err := pgkv.List[model.KbVersion](ctx, p.kv, "kb_versions")
	if err != nil {
		return nil, err
	}
	out := make([]*model.KbVersion, 0, len(versions))
	for _, version := range versions {
		if version.ProjectID == projectID {
			cp := version
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (p *PostgresStore) AddFact(ctx context.Context, fact *model.Fact) error {
	if err := p.requireDraftVersion(ctx, fact.VersionID); err != nil {
		return err
	}
	if fact.ID == "" {
		fact.ID = pgkv.NewID("")
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "facts", fact.ID, *fact)
}

func (p *PostgresStore) AddFAQ(ctx context.Context, faq *model.FAQ) error {
	if err := p.requireDraftVersion(ctx, faq.VersionID); err != nil {
		return err
	}
	if faq.ID == "" {
		faq.ID = pgkv.NewID("")
	}
	if faq.CreatedAt.IsZero() {
		faq.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "faqs", faq.ID, *faq)
}

func (p *PostgresStore) AddAsset(ctx context.Context, asset *model.Asset) error {
	if err := p.requireDraftVersion(ctx, asset.VersionID); err != nil {
		return err
	}
	if asset.ID == "" {
		asset.ID = pgkv.NewID("")
	}
	if asset.Status == "" {
		asset.Status = "pending"
	}
	if asset.CreatedAt.IsZero() {
		asset.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "assets", asset.ID, *asset)
}

func (p *PostgresStore) AddInventory(ctx context.Context, inv *model.Inventory) error {
	if err := p.requireDraftVersion(ctx, inv.VersionID); err != nil {
		return err
	}
	if inv.ID == "" {
		inv.ID = pgkv.NewID("")
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "inventory", inv.ID, *inv)
}

func (p *PostgresStore) AddOffer(ctx context.Context, offer *model.Offer) error {
	if err := p.requireDraftVersion(ctx, offer.VersionID); err != nil {
		return err
	}
	if offer.ID == "" {
		offer.ID = pgkv.NewID("")
	}
	if offer.CreatedAt.IsZero() {
		offer.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "offers", offer.ID, *offer)
}

func (p *PostgresStore) AddDisclaimer(ctx context.Context, disclaimer *model.Disclaimer) error {
	if err := p.requireDraftVersion(ctx, disclaimer.VersionID); err != nil {
		return err
	}
	if disclaimer.ID == "" {
		disclaimer.ID = pgkv.NewID("")
	}
	if disclaimer.CreatedAt.IsZero() {
		disclaimer.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "disclaimers", disclaimer.ID, *disclaimer)
}

func (p *PostgresStore) RecordApprovalEvent(ctx context.Context, event *model.ApprovalEvent) error {
	if event.ID == "" {
		event.ID = pgkv.NewID("")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "approval_events", event.ID, *event)
}

func (p *PostgresStore) RetrieveKb(ctx context.Context, projectID string, queryEmbedding []float32, topK int) (*model.RetrieveResult, error) {
	project, err := p.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.ActiveVersionID == "" {
		return &model.RetrieveResult{}, nil
	}

	type scored struct {
		chunk model.RetrievedChunk
		score float32
	}
	var candidates []scored

	facts, err := pgkv.List[model.Fact](ctx, p.kv, "facts")
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		if fact.VersionID != project.ActiveVersionID {
			continue
		}
		score := pgCosine(queryEmbedding, fact.Embedding)
		candidates = append(candidates, scored{chunk: model.RetrievedChunk{
			VersionID: project.ActiveVersionID,
			ChunkType: "fact",
			Content:   fact.Content,
			Score:     score,
		}, score: score})
	}
	faqs, err := pgkv.List[model.FAQ](ctx, p.kv, "faqs")
	if err != nil {
		return nil, err
	}
	for _, faq := range faqs {
		if faq.VersionID != project.ActiveVersionID {
			continue
		}
		score := pgCosine(queryEmbedding, faq.Embedding)
		candidates = append(candidates, scored{chunk: model.RetrievedChunk{
			VersionID: project.ActiveVersionID,
			ChunkType: "faq",
			Content:   faq.Question + " " + faq.Answer,
			Score:     score,
		}, score: score})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if topK > 0 && len(candidates) > topK {
		candidates = candidates[:topK]
	}
	chunks := make([]model.RetrievedChunk, len(candidates))
	for i, candidate := range candidates {
		chunks[i] = candidate.chunk
	}
	return &model.RetrieveResult{VersionStamp: project.ActiveVersionID, Chunks: chunks}, nil
}

func (p *PostgresStore) AddPronunciation(ctx context.Context, pronunciation *model.Pronunciation) error {
	if pronunciation.ID == "" {
		pronunciation.ID = pgkv.NewID("")
	}
	if pronunciation.CreatedAt.IsZero() {
		pronunciation.CreatedAt = time.Now().UTC()
	}
	return p.kv.Put(ctx, "pronunciations", pronunciation.ID, *pronunciation)
}

func (p *PostgresStore) ListPronunciations(ctx context.Context, tenantID, lang string) ([]*model.Pronunciation, error) {
	items, err := pgkv.List[model.Pronunciation](ctx, p.kv, "pronunciations")
	if err != nil {
		return nil, err
	}
	out := make([]*model.Pronunciation, 0, len(items))
	for _, item := range items {
		if item.TenantID == tenantID && (lang == "" || item.Lang == lang) {
			cp := item
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (p *PostgresStore) requireDraftVersion(ctx context.Context, versionID string) error {
	version, err := p.GetKbVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.Status != model.VersionDraft {
		return fmt.Errorf("cannot add content to version in status %q: only draft versions accept changes", version.Status)
	}
	return nil
}

func pgCosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
