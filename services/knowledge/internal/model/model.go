package model

import "time"

const (
	VersionDraft           = "draft"
	VersionPendingApproval = "pending_approval"
	VersionApproved        = "approved"
	VersionPublished       = "published"
	VersionRejected        = "rejected"
)

type Project struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	Name              string    `json:"name"`
	RERANumber        string    `json:"rera_number,omitempty"`
	BrochureAssetID   string    `json:"brochure_asset_id,omitempty"`
	PriceSheetAssetID string    `json:"price_sheet_asset_id,omitempty"`
	ActiveVersionID   string    `json:"active_version_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type KbVersion struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	VersionNumber  int       `json:"version_number"`
	Status         string    `json:"status"`
	EmbeddingModel string    `json:"embedding_model"`
	SubmittedBy    string    `json:"submitted_by,omitempty"`
	ReviewedBy     string    `json:"reviewed_by,omitempty"`
	ReviewNotes    string    `json:"review_notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Fact struct {
	ID        string         `json:"id"`
	VersionID string         `json:"version_id"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Embedding []float32      `json:"-"`
	CreatedAt time.Time      `json:"created_at"`
}

type FAQ struct {
	ID        string    `json:"id"`
	VersionID string    `json:"version_id"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Embedding []float32 `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type Asset struct {
	ID         string    `json:"id"`
	VersionID  string    `json:"version_id"`
	Name       string    `json:"name"`
	AssetType  string    `json:"asset_type"`
	StorageKey string    `json:"storage_key,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type Inventory struct {
	ID             string    `json:"id"`
	VersionID      string    `json:"version_id"`
	UnitType       string    `json:"unit_type"`
	TotalUnits     int       `json:"total_units"`
	AvailableUnits int       `json:"available_units"`
	PriceMin       float64   `json:"price_min"`
	PriceMax       float64   `json:"price_max"`
	CreatedAt      time.Time `json:"created_at"`
}

type Offer struct {
	ID          string    `json:"id"`
	VersionID   string    `json:"version_id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	ValidUntil  string    `json:"valid_until,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Disclaimer struct {
	ID        string    `json:"id"`
	VersionID string    `json:"version_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type ApprovalEvent struct {
	ID        string    `json:"id"`
	VersionID string    `json:"version_id"`
	Action    string    `json:"action"`
	ActorID   string    `json:"actor_id"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Pronunciation struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Term      string    `json:"term"`
	IPA       string    `json:"ipa,omitempty"`
	Phonetic  string    `json:"phonetic,omitempty"`
	Lang      string    `json:"lang"`
	CreatedAt time.Time `json:"created_at"`
}

type RetrievedChunk struct {
	VersionID string  `json:"version_id"`
	ChunkType string  `json:"chunk_type"` // fact | faq
	Content   string  `json:"content"`
	Score     float32 `json:"score"`
}

type RetrieveResult struct {
	VersionStamp string           `json:"version_stamp"`
	Chunks       []RetrievedChunk `json:"chunks"`
}
