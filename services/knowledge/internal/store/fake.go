package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/knowledge/internal/model"
)

type Fake struct {
	mu              sync.RWMutex
	projects        map[string]*model.Project
	versions        map[string]*model.KbVersion
	facts           map[string]*model.Fact
	faqs            map[string]*model.FAQ
	assets          map[string]*model.Asset
	inventory       map[string]*model.Inventory
	offers          map[string]*model.Offer
	disclaimers     map[string]*model.Disclaimer
	approvalHistory []*model.ApprovalEvent
	pronunciations  map[string]*model.Pronunciation
}

func NewFake() *Fake {
	return &Fake{
		projects:       make(map[string]*model.Project),
		versions:       make(map[string]*model.KbVersion),
		facts:          make(map[string]*model.Fact),
		faqs:           make(map[string]*model.FAQ),
		assets:         make(map[string]*model.Asset),
		inventory:      make(map[string]*model.Inventory),
		offers:         make(map[string]*model.Offer),
		disclaimers:    make(map[string]*model.Disclaimer),
		pronunciations: make(map[string]*model.Pronunciation),
	}
}

func newID() string { return uuid.New().String() }

func (f *Fake) CreateProject(ctx context.Context, p *model.Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == "" {
		p.ID = newID()
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	clone := *p
	f.projects[p.ID] = &clone
	return nil
}

func (f *Fake) UpdateProject(ctx context.Context, p *model.Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[p.ID]; !ok {
		return errors.New("project not found")
	}
	p.UpdatedAt = time.Now()
	clone := *p
	f.projects[p.ID] = &clone
	return nil
}

func (f *Fake) GetProject(ctx context.Context, id string) (*model.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.projects[id]
	if !ok {
		return nil, errors.New("project not found")
	}
	clone := *p
	return &clone, nil
}

func (f *Fake) ListProjects(ctx context.Context, tenantID string) ([]*model.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*model.Project
	for _, p := range f.projects {
		if p.TenantID == tenantID {
			clone := *p
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (f *Fake) CreateKbVersion(ctx context.Context, v *model.KbVersion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v.ID == "" {
		v.ID = newID()
	}
	if v.EmbeddingModel == "" {
		v.EmbeddingModel = "BAAI/bge-m3"
	}
	v.CreatedAt = time.Now()
	v.UpdatedAt = time.Now()
	// auto-increment version number
	maxVer := 0
	for _, existing := range f.versions {
		if existing.ProjectID == v.ProjectID && existing.VersionNumber > maxVer {
			maxVer = existing.VersionNumber
		}
	}
	v.VersionNumber = maxVer + 1
	clone := *v
	f.versions[v.ID] = &clone
	return nil
}

func (f *Fake) GetKbVersion(ctx context.Context, id string) (*model.KbVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	v, ok := f.versions[id]
	if !ok {
		return nil, errors.New("version not found")
	}
	clone := *v
	return &clone, nil
}

func (f *Fake) UpdateKbVersionStatus(ctx context.Context, id, status, reviewedBy, notes string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.versions[id]
	if !ok {
		return errors.New("version not found")
	}
	v.Status = status
	if reviewedBy != "" {
		v.ReviewedBy = reviewedBy
	}
	if notes != "" {
		v.ReviewNotes = notes
	}
	v.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) SetActiveVersion(ctx context.Context, projectID, versionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.projects[projectID]
	if !ok {
		return errors.New("project not found")
	}
	p.ActiveVersionID = versionID
	p.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) ListKbVersions(ctx context.Context, projectID string) ([]*model.KbVersion, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*model.KbVersion
	for _, v := range f.versions {
		if v.ProjectID == projectID {
			clone := *v
			out = append(out, &clone)
		}
	}
	return out, nil
}

// addToVersion checks that the version exists and is in draft status.
// Caller must hold f.mu (write lock).
func (f *Fake) addToVersion(ctx context.Context, versionID string) (*model.KbVersion, error) {
	v, ok := f.versions[versionID]
	if !ok {
		return nil, errors.New("version not found")
	}
	if v.Status != model.VersionDraft {
		return nil, fmt.Errorf("cannot add content to version in status %q: only draft versions accept changes", v.Status)
	}
	return v, nil
}

func (f *Fake) AddFact(ctx context.Context, fact *model.Fact) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.addToVersion(ctx, fact.VersionID); err != nil {
		return err
	}
	if fact.ID == "" {
		fact.ID = newID()
	}
	fact.CreatedAt = time.Now()
	clone := *fact
	f.facts[fact.ID] = &clone
	return nil
}

func (f *Fake) AddFAQ(ctx context.Context, faq *model.FAQ) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.addToVersion(ctx, faq.VersionID); err != nil {
		return err
	}
	if faq.ID == "" {
		faq.ID = newID()
	}
	faq.CreatedAt = time.Now()
	clone := *faq
	f.faqs[faq.ID] = &clone
	return nil
}

func (f *Fake) AddAsset(ctx context.Context, a *model.Asset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.addToVersion(ctx, a.VersionID); err != nil {
		return err
	}
	if a.ID == "" {
		a.ID = newID()
	}
	a.CreatedAt = time.Now()
	if a.Status == "" {
		a.Status = "pending"
	}
	clone := *a
	f.assets[a.ID] = &clone
	return nil
}

func (f *Fake) AddInventory(ctx context.Context, inv *model.Inventory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.addToVersion(ctx, inv.VersionID); err != nil {
		return err
	}
	if inv.ID == "" {
		inv.ID = newID()
	}
	inv.CreatedAt = time.Now()
	clone := *inv
	f.inventory[inv.ID] = &clone
	return nil
}

func (f *Fake) AddOffer(ctx context.Context, o *model.Offer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.addToVersion(ctx, o.VersionID); err != nil {
		return err
	}
	if o.ID == "" {
		o.ID = newID()
	}
	o.CreatedAt = time.Now()
	clone := *o
	f.offers[o.ID] = &clone
	return nil
}

func (f *Fake) AddDisclaimer(ctx context.Context, d *model.Disclaimer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.addToVersion(ctx, d.VersionID); err != nil {
		return err
	}
	if d.ID == "" {
		d.ID = newID()
	}
	d.CreatedAt = time.Now()
	clone := *d
	f.disclaimers[d.ID] = &clone
	return nil
}

func (f *Fake) RecordApprovalEvent(ctx context.Context, e *model.ApprovalEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == "" {
		e.ID = newID()
	}
	e.CreatedAt = time.Now()
	clone := *e
	f.approvalHistory = append(f.approvalHistory, &clone)
	return nil
}

func cosine(a, b []float32) float32 {
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

func (f *Fake) RetrieveKb(ctx context.Context, projectID string, queryEmbedding []float32, topK int) (*model.RetrieveResult, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	p, ok := f.projects[projectID]
	if !ok {
		return nil, errors.New("project not found")
	}
	if p.ActiveVersionID == "" {
		return &model.RetrieveResult{VersionStamp: "", Chunks: nil}, nil
	}
	activeVID := p.ActiveVersionID

	type scored struct {
		chunk model.RetrievedChunk
		score float32
	}
	var candidates []scored

	for _, fact := range f.facts {
		if fact.VersionID != activeVID {
			continue
		}
		score := cosine(queryEmbedding, fact.Embedding)
		candidates = append(candidates, scored{
			chunk: model.RetrievedChunk{
				VersionID: activeVID,
				ChunkType: "fact",
				Content:   fact.Content,
				Score:     score,
			},
			score: score,
		})
	}
	for _, faq := range f.faqs {
		if faq.VersionID != activeVID {
			continue
		}
		score := cosine(queryEmbedding, faq.Embedding)
		candidates = append(candidates, scored{
			chunk: model.RetrievedChunk{
				VersionID: activeVID,
				ChunkType: "faq",
				Content:   faq.Question + " " + faq.Answer,
				Score:     score,
			},
			score: score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if topK > 0 && len(candidates) > topK {
		candidates = candidates[:topK]
	}
	chunks := make([]model.RetrievedChunk, len(candidates))
	for i, c := range candidates {
		chunks[i] = c.chunk
	}
	return &model.RetrieveResult{
		VersionStamp: activeVID,
		Chunks:       chunks,
	}, nil
}

func (f *Fake) AddPronunciation(ctx context.Context, p *model.Pronunciation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == "" {
		p.ID = newID()
	}
	p.CreatedAt = time.Now()
	clone := *p
	f.pronunciations[p.ID] = &clone
	return nil
}

func (f *Fake) ListPronunciations(ctx context.Context, tenantID, lang string) ([]*model.Pronunciation, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*model.Pronunciation
	for _, p := range f.pronunciations {
		if p.TenantID == tenantID && (lang == "" || p.Lang == lang) {
			clone := *p
			out = append(out, &clone)
		}
	}
	return out, nil
}
