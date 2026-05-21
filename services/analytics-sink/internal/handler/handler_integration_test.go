//go:build integration

package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/lead/services/analytics-sink/internal/handler"
	"github.com/lead/services/analytics-sink/internal/model"
	"github.com/lead/services/analytics-sink/internal/service"
	"github.com/lead/services/analytics-sink/internal/store"
)

func TestReportsEndpointUsesClickHouse(t *testing.T) {
	url := os.Getenv("CLICKHOUSE_URL")
	if url == "" {
		t.Skip("set CLICKHOUSE_URL to run ClickHouse integration test")
	}
	st, err := store.NewClickHouse(url)
	if err != nil {
		t.Fatalf("new clickhouse store: %v", err)
	}
	defer st.Close()
	svc := service.New(st)
	tenantID := "tenant-c11-http-" + time.Now().Format("20060102150405")
	if _, err := svc.Ingest(t.Context(), model.CanonicalEvent{
		EventID:    "evt-http",
		TenantID:   tenantID,
		Type:       "call.completed",
		Payload:    map[string]any{"duration_s": 12},
		OccurredAt: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	mux := http.NewServeMux()
	handler.New(svc).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/reports/daily?tenant_id="+tenantID, nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var report model.ReportResponse
	if err := json.NewDecoder(res.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(report.Rows) != 1 || report.Rows[0].Count != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}
