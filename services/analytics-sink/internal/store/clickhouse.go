package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	chclient "github.com/lead/services/analytics-sink/internal/clickhouse"
	"github.com/lead/services/analytics-sink/internal/model"
)

type ClickHouseStore struct {
	client *chclient.Client
	now    func() time.Time
}

func NewClickHouse(rawURL string) (*ClickHouseStore, error) {
	client, err := chclient.NewClient(rawURL)
	if err != nil {
		return nil, err
	}
	return &ClickHouseStore{client: client, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *ClickHouseStore) Close() {
	_ = s.client.Close()
}

func (s *ClickHouseStore) Client() *chclient.Client {
	return s.client
}

func (s *ClickHouseStore) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *ClickHouseStore) InsertFact(ctx context.Context, row model.FactRow) (bool, error) {
	if row.InsertedAt.IsZero() {
		row.InsertedAt = s.now()
	}
	if row.OccurredAt.IsZero() {
		row.OccurredAt = row.InsertedAt
	}
	if err := s.InsertBatch(ctx, row.Fact, []model.FactRow{row}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ClickHouseStore) InsertBatch(ctx context.Context, table string, rows []model.FactRow) error {
	converted := make([]chclient.FactRow, 0, len(rows))
	for _, row := range rows {
		converted = append(converted, chclient.FactRow{
			EventID:    row.EventID,
			TenantID:   row.TenantID,
			CampaignID: row.CampaignID,
			ProjectID:  row.ProjectID,
			UserID:     row.UserID,
			Metric:     row.Metric,
			Value:      row.Value,
			Payload:    row.Payload,
			OccurredAt: row.OccurredAt,
			InsertedAt: row.InsertedAt,
			Version:    row.Version,
		})
	}
	return s.client.InsertBatch(ctx, table, converted)
}

func (s *ClickHouseStore) CountFact(ctx context.Context, tenantID, fact string) (int, error) {
	if !chclient.IsFactTable(fact) {
		return 0, fmt.Errorf("unsupported fact table %q", fact)
	}
	var count uint64
	err := s.client.QueryRow(ctx,
		fmt.Sprintf("SELECT count() FROM %s FINAL WHERE tenant_id = ?", s.client.Qualified(fact)),
		tenantID,
	).Scan(&count)
	return int(count), err
}

func (s *ClickHouseStore) QueryReport(ctx context.Context, tenantID, report string) (model.ReportResponse, error) {
	spec, err := reportSpec(report)
	if err != nil {
		return model.ReportResponse{}, err
	}
	query := fmt.Sprintf(`
SELECT dimension, count() AS count, sum(value) AS value
FROM (
	SELECT %s AS dimension, value
	FROM (%s)
	WHERE tenant_id = ?%s
)
GROUP BY dimension
ORDER BY dimension
`, spec.dimensionExpr, s.factUnion(), spec.whereSuffix)
	rows, err := s.client.Query(ctx, query, tenantID)
	if err != nil {
		return model.ReportResponse{}, err
	}
	defer rows.Close()
	out := []model.ReportRow{}
	for rows.Next() {
		var row model.ReportRow
		var count uint64
		if err := rows.Scan(&row.Dimension, &count, &row.Value); err != nil {
			return model.ReportResponse{}, err
		}
		row.Count = int64(count)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return model.ReportResponse{}, err
	}
	freshness, err := s.freshness(ctx, tenantID)
	if err != nil {
		return model.ReportResponse{}, err
	}
	return model.ReportResponse{TenantID: tenantID, Report: report, FreshnessS: freshness, Rows: out}, nil
}

func (s *ClickHouseStore) freshness(ctx context.Context, tenantID string) (int64, error) {
	query := fmt.Sprintf(`
SELECT if(count() = 0, 0, dateDiff('second', max(inserted_at), now()))
FROM (%s)
WHERE tenant_id = ?
`, s.factUnion())
	var freshness int64
	if err := s.client.QueryRow(ctx, query, tenantID).Scan(&freshness); err != nil {
		return 0, err
	}
	if freshness < 0 {
		return 0, nil
	}
	return freshness, nil
}

func (s *ClickHouseStore) factUnion() string {
	tables := chclient.FactTableNames()
	sort.Strings(tables)
	parts := make([]string, 0, len(tables))
	for _, table := range tables {
		parts = append(parts, fmt.Sprintf(`
SELECT
	'%s' AS fact,
	tenant_id,
	event_id,
	campaign_id,
	project_id,
	user_id,
	metric,
	value,
	payload,
	occurred_at,
	inserted_at
FROM %s FINAL`, table, s.client.Qualified(table)))
	}
	return strings.Join(parts, "\nUNION ALL\n")
}

type reportQuerySpec struct {
	dimensionExpr string
	whereSuffix   string
}

func reportSpec(report string) (reportQuerySpec, error) {
	switch report {
	case "", "daily":
		return reportQuerySpec{dimensionExpr: "toString(toDate(occurred_at))"}, nil
	case "monthly":
		return reportQuerySpec{dimensionExpr: "formatDateTime(occurred_at, '%Y-%m')"}, nil
	case "campaign":
		return reportQuerySpec{dimensionExpr: "if(campaign_id = '', 'unassigned', campaign_id)"}, nil
	case "source":
		return reportQuerySpec{dimensionExpr: "if(JSONExtractString(payload, 'source') = '', 'unknown', JSONExtractString(payload, 'source'))"}, nil
	case "project":
		return reportQuerySpec{dimensionExpr: "if(project_id = '', 'unassigned', project_id)"}, nil
	case "site-visit":
		return reportQuerySpec{dimensionExpr: "if(project_id = '', 'unassigned', project_id)", whereSuffix: " AND fact = 'fact_site_visits'"}, nil
	case "salesperson":
		return reportQuerySpec{dimensionExpr: "if(user_id = '', 'unassigned', user_id)"}, nil
	case "whatsapp":
		return reportQuerySpec{dimensionExpr: "metric", whereSuffix: " AND fact = 'fact_whatsapp'"}, nil
	case "cost":
		return reportQuerySpec{dimensionExpr: "metric", whereSuffix: " AND fact = 'fact_costs'"}, nil
	case "ai-quality":
		return reportQuerySpec{dimensionExpr: "metric", whereSuffix: " AND fact = 'fact_ai_outputs'"}, nil
	default:
		return reportQuerySpec{}, fmt.Errorf("unsupported report %q", report)
	}
}
