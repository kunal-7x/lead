package handler

import (
	"context"

	"github.com/lead/services/knowledge/internal/embed"
	"github.com/lead/services/knowledge/internal/index"
	"github.com/lead/services/knowledge/internal/store"
	"github.com/lead/services/knowledge/internal/vector"
)

// indexerAdapter wires the kb.Service's VersionIndexer interface to the
// concrete *index.Indexer without leaking the index package into kb (kb is
// imported by handler tests, which would otherwise need Qdrant). nil-safe.
type indexerAdapter struct {
	inner *index.Indexer
}

func newIndexerAdapter(s store.Store, e embed.Provider, q *vector.Client) *indexerAdapter {
	if e == nil || q == nil {
		return nil
	}
	return &indexerAdapter{inner: index.New(s, e, q)}
}

func (a *indexerAdapter) IndexVersion(ctx context.Context, tenantID, projectID, versionID string) (int, error) {
	if a == nil || a.inner == nil {
		return 0, nil
	}
	return a.inner.IndexVersion(ctx, tenantID, projectID, versionID)
}
