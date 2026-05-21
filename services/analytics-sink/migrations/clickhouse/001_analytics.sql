CREATE DATABASE IF NOT EXISTS evs;

CREATE TABLE IF NOT EXISTS evs.fact_calls (
  tenant_id String,
  event_id String,
  campaign_id String,
  project_id String,
  user_id String,
  metric String,
  value Float64,
  payload String,
  occurred_at DateTime64(3),
  inserted_at DateTime64(3),
  version UInt64
) ENGINE = ReplacingMergeTree(version)
ORDER BY (tenant_id, event_id);

CREATE TABLE IF NOT EXISTS evs.fact_call_turns AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_whatsapp AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_site_visits AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_handoffs AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_costs AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_lead_status_changes AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_ai_outputs AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);
CREATE TABLE IF NOT EXISTS evs.fact_kb_retrievals AS evs.fact_calls ENGINE = ReplacingMergeTree(version) ORDER BY (tenant_id, event_id);

CREATE TABLE IF NOT EXISTS evs.dim_tenants (
  tenant_id String,
  name String,
  updated_at DateTime64(3)
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY tenant_id;

CREATE TABLE IF NOT EXISTS evs.dim_campaigns (
  tenant_id String,
  campaign_id String,
  name String,
  updated_at DateTime64(3)
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (tenant_id, campaign_id);

CREATE TABLE IF NOT EXISTS evs.dim_projects (
  tenant_id String,
  project_id String,
  name String,
  updated_at DateTime64(3)
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (tenant_id, project_id);

CREATE TABLE IF NOT EXISTS evs.dim_users (
  tenant_id String,
  user_id String,
  name String,
  role String,
  updated_at DateTime64(3)
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (tenant_id, user_id);

CREATE TABLE IF NOT EXISTS evs.dim_kb_versions (
  tenant_id String,
  kb_version_id String,
  project_id String,
  status String,
  updated_at DateTime64(3)
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (tenant_id, kb_version_id);

CREATE MATERIALIZED VIEW IF NOT EXISTS evs.mv_daily_report
ENGINE = SummingMergeTree
ORDER BY (tenant_id, day)
AS SELECT tenant_id, toDate(occurred_at) AS day, count() AS events, sum(value) AS value
FROM evs.fact_calls
GROUP BY tenant_id, day;

CREATE MATERIALIZED VIEW IF NOT EXISTS evs.mv_campaign_report
ENGINE = SummingMergeTree
ORDER BY (tenant_id, campaign_id)
AS SELECT tenant_id, campaign_id, count() AS events, sum(value) AS value
FROM evs.fact_calls
GROUP BY tenant_id, campaign_id;

CREATE MATERIALIZED VIEW IF NOT EXISTS evs.mv_cost_report
ENGINE = SummingMergeTree
ORDER BY (tenant_id, metric)
AS SELECT tenant_id, metric, count() AS events, sum(value) AS value
FROM evs.fact_costs
GROUP BY tenant_id, metric;
