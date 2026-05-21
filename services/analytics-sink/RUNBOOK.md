# analytics-sink Runbook

Purpose: ingest operational events and write reporting-ready records to ClickHouse.

Dependencies: NATS, ClickHouse, tenant metadata, analytics schemas.

Local setup:
```powershell
docker run -d --name clickhouse -p 8123:8123 -p 9000:9000 `
  -e CLICKHOUSE_USER=default -e CLICKHOUSE_PASSWORD=clickhouse `
  clickhouse/clickhouse-server:latest
$env:CLICKHOUSE_URL = "http://default:clickhouse@localhost:8123"
powershell -ExecutionPolicy Bypass -File ..\..\scripts\migrate_clickhouse.ps1
```

Runtime store selection:
- Default: ClickHouse via `CLICKHOUSE_URL` (`http://localhost:8123` fallback).
- Demo only: `DEMO_MODE=1` or `ANALYTICS_STORE=fake`.
- Legacy C1 fallback: `ANALYTICS_STORE=postgres` with `DATABASE_URL`.

Paging signals: event lag above SLO, ClickHouse write failures, malformed event spike, process restarts.

Common fixes: verify `CLICKHOUSE_URL`, check NATS connectivity, replay DLQ events after schema fixes, roll back the last deployed image if write errors begin after release.

Dashboards and logs: analytics ingest rate, ClickHouse insert latency, DLQ count, service JSON logs.

Rollback: deploy previous image and pause new analytics consumers until replay is safe.
