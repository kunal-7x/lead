package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

const defaultDatabase = "evs"

var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var factTables = map[string]struct{}{
	"fact_calls":               {},
	"fact_call_turns":          {},
	"fact_whatsapp":            {},
	"fact_handoffs":            {},
	"fact_site_visits":         {},
	"fact_costs":               {},
	"fact_lead_status_changes": {},
	"fact_ai_outputs":          {},
	"fact_kb_retrievals":       {},
}

var factColumns = []ch.ColumnNameAndType{
	{Name: "tenant_id", Type: "String"},
	{Name: "event_id", Type: "String"},
	{Name: "campaign_id", Type: "String"},
	{Name: "project_id", Type: "String"},
	{Name: "user_id", Type: "String"},
	{Name: "metric", Type: "String"},
	{Name: "value", Type: "Float64"},
	{Name: "payload", Type: "String"},
	{Name: "occurred_at", Type: "DateTime64(3)"},
	{Name: "inserted_at", Type: "DateTime64(3)"},
	{Name: "version", Type: "UInt64"},
}

type FactRow struct {
	EventID    string
	TenantID   string
	CampaignID string
	ProjectID  string
	UserID     string
	Metric     string
	Value      float64
	Payload    map[string]any
	OccurredAt time.Time
	InsertedAt time.Time
	Version    int64
}

type Client struct {
	conn     ch.Conn
	database string
}

func NewClient(rawURL string) (*Client, error) {
	if strings.TrimSpace(rawURL) == "" {
		rawURL = "http://localhost:8123"
	}
	opt, err := ch.ParseDSN(rawURL)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: parse url: %w", err)
	}
	if opt.Auth.Database == "" {
		opt.Auth.Database = defaultDatabase
	}
	if !identPattern.MatchString(opt.Auth.Database) {
		return nil, fmt.Errorf("clickhouse: invalid database %q", opt.Auth.Database)
	}
	opt.MaxOpenConns = 10
	opt.MaxIdleConns = 10
	opt.HttpMaxConnsPerHost = 10
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 5 * time.Second
	}
	if opt.ReadTimeout == 0 {
		opt.ReadTimeout = 30 * time.Second
	}
	if opt.ConnMaxLifetime == 0 {
		opt.ConnMaxLifetime = time.Hour
	}

	conn, err := ch.Open(opt)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("clickhouse: ping: %w", err)
	}
	return &Client{conn: conn, database: opt.Auth.Database}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Database() string {
	return c.database
}

func (c *Client) Conn() ch.Conn {
	return c.conn
}

func FactTableNames() []string {
	out := make([]string, 0, len(factTables))
	for table := range factTables {
		out = append(out, table)
	}
	return out
}

func IsFactTable(table string) bool {
	_, ok := factTables[table]
	return ok
}

func (c *Client) InsertBatch(ctx context.Context, table string, rows []FactRow) error {
	if len(rows) == 0 {
		return nil
	}
	if !IsFactTable(table) {
		return fmt.Errorf("clickhouse: unsupported fact table %q", table)
	}
	ctx = ch.Context(ctx,
		ch.WithSettings(ch.Settings{"async_insert": 1, "wait_for_async_insert": 1}),
		ch.WithColumnNamesAndTypes(factColumns),
	)
	batch, err := c.conn.PrepareBatch(ctx, fmt.Sprintf("INSERT INTO %s (%s)", c.qualified(table), factColumnList()))
	if err != nil {
		return fmt.Errorf("clickhouse: prepare %s: %w", table, err)
	}
	defer batch.Close()
	for _, row := range rows {
		payload, err := json.Marshal(row.Payload)
		if err != nil {
			return fmt.Errorf("clickhouse: marshal payload %s: %w", row.EventID, err)
		}
		if row.InsertedAt.IsZero() {
			row.InsertedAt = time.Now().UTC()
		}
		if row.OccurredAt.IsZero() {
			row.OccurredAt = row.InsertedAt
		}
		version := row.Version
		if version <= 0 {
			version = row.OccurredAt.UnixNano()
		}
		if version <= 0 {
			version = 1
		}
		if err := batch.Append(
			row.TenantID,
			row.EventID,
			row.CampaignID,
			row.ProjectID,
			row.UserID,
			row.Metric,
			row.Value,
			string(payload),
			row.OccurredAt.UTC(),
			row.InsertedAt.UTC(),
			uint64(version),
		); err != nil {
			return fmt.Errorf("clickhouse: append %s/%s: %w", table, row.EventID, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("clickhouse: send %s: %w", table, err)
	}
	return nil
}

func (c *Client) Exec(ctx context.Context, query string, args ...any) error {
	return c.conn.Exec(ctx, query, args...)
}

func (c *Client) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return c.conn.Query(ctx, query, args...)
}

func (c *Client) QueryRow(ctx context.Context, query string, args ...any) Row {
	return c.conn.QueryRow(ctx, query, args...)
}

func (c *Client) Qualified(table string) string {
	return c.qualified(table)
}

func (c *Client) qualified(table string) string {
	if !identPattern.MatchString(c.database) || !identPattern.MatchString(table) {
		panic("invalid ClickHouse identifier")
	}
	return c.database + "." + table
}

func factColumnList() string {
	names := make([]string, 0, len(factColumns))
	for _, col := range factColumns {
		names = append(names, col.Name)
	}
	return strings.Join(names, ", ")
}

type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

type Row interface {
	Scan(dest ...any) error
}
