# analytics-sink Runbook

Purpose: ingest operational events and write reporting-ready records to ClickHouse.

Dependencies: NATS, ClickHouse, tenant metadata, analytics schemas.

Paging signals: event lag above SLO, ClickHouse write failures, malformed event spike, process restarts.

Common fixes: verify `CLICKHOUSE_URL`, check NATS connectivity, replay DLQ events after schema fixes, roll back the last deployed image if write errors begin after release.

Dashboards and logs: analytics ingest rate, ClickHouse insert latency, DLQ count, service JSON logs.

Rollback: deploy previous image and pause new analytics consumers until replay is safe.
