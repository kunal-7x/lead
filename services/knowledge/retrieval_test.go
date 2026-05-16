//go:build pgvector

package knowledge_test

import (
	"context"
	"testing"

	"github.com/lead/services/knowledge/internal/kb"
	"github.com/lead/services/knowledge/internal/model"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEmbedding creates a unit vector of the given dimension with a 1.0 at position idx.
func makeEmbedding(dim, idx int) []float32 {
	v := make([]float32, dim)
	if idx < dim {
		v[idx] = 1.0
	}
	return v
}

func setupRetrievalTest(t *testing.T) (*store.Fake, *kb.Service, string, string, string) {
	t.Helper()
	ctx := context.Background()
	s := store.NewFake()
	svc := kb.New(s)

	p := &model.Project{
		TenantID:          "tenant-ret",
		Name:              "Retrieval Project",
		RERANumber:        "RERA-200",
		BrochureAssetID:   "brochure-ret",
		PriceSheetAssetID: "ps-ret",
	}
	require.NoError(t, s.CreateProject(ctx, p))

	// Version 1
	v1 := &model.KbVersion{ProjectID: p.ID, Status: model.VersionDraft}
	require.NoError(t, s.CreateKbVersion(ctx, v1))
	require.NoError(t, s.AddFact(ctx, &model.Fact{
		VersionID: v1.ID,
		Content:   "fact from v1",
		Embedding: makeEmbedding(4, 0),
	}))

	// Publish v1
	require.NoError(t, svc.SubmitForApproval(ctx, v1.ID, "actor"))
	require.NoError(t, svc.Approve(ctx, v1.ID, "reviewer"))
	require.NoError(t, svc.Publish(ctx, v1.ID, "actor"))

	// Version 2 (draft, not yet published)
	v2 := &model.KbVersion{ProjectID: p.ID, Status: model.VersionDraft}
	require.NoError(t, s.CreateKbVersion(ctx, v2))
	require.NoError(t, s.AddFact(ctx, &model.Fact{
		VersionID: v2.ID,
		Content:   "fact from v2",
		Embedding: makeEmbedding(4, 1),
	}))

	return s, svc, p.ID, v1.ID, v2.ID
}

func TestRetrieval_ActiveVersionOnly(t *testing.T) {
	ctx := context.Background()
	s, _, projectID, v1ID, _ := setupRetrievalTest(t)

	query := makeEmbedding(4, 0) // closest to v1 fact
	result, err := s.RetrieveKb(ctx, projectID, query, 10)
	require.NoError(t, err)

	assert.Equal(t, v1ID, result.VersionStamp)
	require.NotEmpty(t, result.Chunks)
	// All chunks must come from v1
	for _, chunk := range result.Chunks {
		assert.Equal(t, v1ID, chunk.VersionID, "chunk should be from v1")
	}
	// v2 fact must not appear
	for _, chunk := range result.Chunks {
		assert.NotEqual(t, "fact from v2", chunk.Content)
	}
}

func TestRetrieval_AfterSwappingActiveVersion(t *testing.T) {
	ctx := context.Background()
	s, svc, projectID, _, v2ID := setupRetrievalTest(t)

	// Publish v2 to make it the active version
	require.NoError(t, svc.SubmitForApproval(ctx, v2ID, "actor"))
	require.NoError(t, svc.Approve(ctx, v2ID, "reviewer"))
	require.NoError(t, svc.Publish(ctx, v2ID, "actor"))

	query := makeEmbedding(4, 1) // closest to v2 fact
	result, err := s.RetrieveKb(ctx, projectID, query, 10)
	require.NoError(t, err)

	assert.Equal(t, v2ID, result.VersionStamp)
	require.NotEmpty(t, result.Chunks)
	// All chunks must come from v2
	for _, chunk := range result.Chunks {
		assert.Equal(t, v2ID, chunk.VersionID, "chunk should be from v2")
	}
	// v1 fact must not appear
	for _, chunk := range result.Chunks {
		assert.NotEqual(t, "fact from v1", chunk.Content)
	}
}
