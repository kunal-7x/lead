package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/lead/services/analytics-sink/internal/model"
)

type Fake struct {
	mu   sync.Mutex
	rows map[string]model.FactRow
	now  func() time.Time
}

func NewFake() *Fake {
	return &Fake{rows: make(map[string]model.FactRow), now: func() time.Time { return time.Now().UTC() }}
}

func (f *Fake) SetNow(now func() time.Time) {
	if now != nil {
		f.now = now
	}
}

func (f *Fake) InsertFact(_ context.Context, row model.FactRow) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := row.TenantID + ":" + row.Fact + ":" + row.EventID
	if _, exists := f.rows[key]; exists {
		return false, nil
	}
	if row.InsertedAt.IsZero() {
		row.InsertedAt = f.now()
	}
	f.rows[key] = row
	return true, nil
}

func (f *Fake) CountFact(_ context.Context, tenantID, fact string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, row := range f.rows {
		if row.TenantID == tenantID && row.Fact == fact {
			count++
		}
	}
	return count, nil
}

func (f *Fake) QueryReport(_ context.Context, tenantID, report string) (model.ReportResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	group := make(map[string]model.ReportRow)
	var newest time.Time
	for _, row := range f.rows {
		if row.TenantID != tenantID {
			continue
		}
		if !row.InsertedAt.IsZero() && row.InsertedAt.After(newest) {
			newest = row.InsertedAt
		}
		dim := dimensionFor(report, row)
		item := group[dim]
		item.Dimension = dim
		item.Count++
		item.Value += row.Value
		group[dim] = item
	}
	rows := make([]model.ReportRow, 0, len(group))
	for _, row := range group {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Dimension < rows[j].Dimension })
	freshness := int64(0)
	if !newest.IsZero() {
		freshness = int64(f.now().Sub(newest).Seconds())
	}
	return model.ReportResponse{TenantID: tenantID, Report: report, FreshnessS: freshness, Rows: rows}, nil
}

func dimensionFor(report string, row model.FactRow) string {
	switch report {
	case "campaign":
		return fallback(row.CampaignID, "unassigned")
	case "project", "site-visit":
		return fallback(row.ProjectID, "unassigned")
	case "salesperson":
		return fallback(row.UserID, "unassigned")
	case "whatsapp":
		return row.Fact
	case "cost":
		return row.Metric
	case "ai-quality":
		return row.Fact
	case "source":
		if source, ok := row.Payload["source"].(string); ok && source != "" {
			return source
		}
		return "unknown"
	case "monthly":
		return row.OccurredAt.Format("2006-01")
	default:
		return row.OccurredAt.Format("2006-01-02")
	}
}

func fallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
