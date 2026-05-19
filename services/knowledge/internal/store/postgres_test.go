//go:build integration

package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/lead/services/knowledge/internal/model"
)

func TestPostgresStoreKnowledgeRoundTrip(t *testing.T) {
	st := newTestPostgres(t)
	defer st.Close()

	ctx := context.Background()
	project := &model.Project{TenantID: testID("tenant"), Name: "Project " + testID("p")}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	version := &model.KbVersion{ProjectID: project.ID, Status: model.VersionDraft}
	if err := st.CreateKbVersion(ctx, version); err != nil {
		t.Fatalf("create version: %v", err)
	}
	if err := st.AddFact(ctx, &model.Fact{VersionID: version.ID, Content: "Pool facing tower", Embedding: []float32{1, 0}}); err != nil {
		t.Fatalf("add fact: %v", err)
	}
	if err := st.SetActiveVersion(ctx, project.ID, version.ID); err != nil {
		t.Fatalf("set active: %v", err)
	}
	result, err := st.RetrieveKb(ctx, project.ID, []float32{1, 0}, 1)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.Chunks) != 1 {
		t.Fatalf("chunks len = %d, want 1", len(result.Chunks))
	}
}

func newTestPostgres(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	st, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	return st
}

func testID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
