package store

import (
	"context"
	"sort"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/analytics-sink/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_analytics_sink")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) InsertFact(ctx context.Context, row model.FactRow) (bool, error) {
	if row.InsertedAt.IsZero() {
		row.InsertedAt = time.Now().UTC()
	}
	return p.kv.Insert(ctx, "facts", row.TenantID+":"+row.Fact+":"+row.EventID, row)
}

func (p *PostgresStore) CountFact(ctx context.Context, tenantID, fact string) (int, error) {
	rows, err := pgkv.List[model.FactRow](ctx, p.kv, "facts")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, row := range rows {
		if row.TenantID == tenantID && row.Fact == fact {
			count++
		}
	}
	return count, nil
}

func (p *PostgresStore) QueryReport(ctx context.Context, tenantID, report string) (model.ReportResponse, error) {
	rows, err := pgkv.List[model.FactRow](ctx, p.kv, "facts")
	if err != nil {
		return model.ReportResponse{}, err
	}
	group := make(map[string]model.ReportRow)
	var newest time.Time
	for _, row := range rows {
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
	out := make([]model.ReportRow, 0, len(group))
	for _, row := range group {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dimension < out[j].Dimension })
	var freshness int64
	if !newest.IsZero() {
		freshness = int64(time.Since(newest).Seconds())
	}
	return model.ReportResponse{TenantID: tenantID, Report: report, FreshnessS: freshness, Rows: out}, nil
}
