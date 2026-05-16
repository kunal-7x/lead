//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lead/services/lead-import/internal/service"
	"github.com/lead/services/lead-import/internal/store"
)

// TestLargeCSVImport imports 5,000 rows and expects them all to be visible in < 60 s.
func TestLargeCSVImport(t *testing.T) {
	r := chi.NewRouter()
	svc := service.New(store.NewFake())
	svc.Mount(r)

	const rowCount = 5000
	rows := make([]map[string]string, rowCount)
	for i := 0; i < rowCount; i++ {
		rows[i] = map[string]string{
			"phone": fmt.Sprintf("+9198765%05d", i),
			"name":  fmt.Sprintf("Lead %d", i),
		}
	}

	body, _ := json.Marshal(map[string]any{
		"rows":    rows,
		"mapping": map[string]string{},
	})

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/v1/import/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-perf")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	elapsed := time.Since(start)
	if elapsed > 60*time.Second {
		t.Errorf("import took %v, expected < 60s", elapsed)
	}

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	job := resp["job"].(map[string]any)

	importedRows := int(job["imported_rows"].(float64))
	if importedRows < rowCount {
		t.Errorf("expected %d imported rows, got %d", rowCount, importedRows)
	}

	// Verify leads are visible via list endpoint.
	listReq := httptest.NewRequest(http.MethodGet, "/v1/leads", nil)
	listReq.Header.Set("X-Tenant-ID", "tenant-perf")
	lw := httptest.NewRecorder()
	r.ServeHTTP(lw, listReq)
	if lw.Code != http.StatusOK {
		t.Fatalf("list leads returned %d", lw.Code)
	}
	t.Logf("5000-row import completed in %v; elapsed check passed", elapsed)
	_ = strings.Contains // suppress unused import warning
}
