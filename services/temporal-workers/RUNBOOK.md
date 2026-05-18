# temporal-workers Runbook

Purpose: run long-lived campaign, scheduling, billing, and operational workflows.

Dependencies: Temporal, Postgres, NATS, campaign, scheduler.

Paging signals: workflow failures, task queue backlog, worker panics, activity timeout spike.

Common fixes: inspect failed workflows, restart workers, replay safe activities, increase activity timeout when provider outage is known.

Dashboards and logs: task queue depth, workflow failure count, activity retries, worker restarts.

Rollback: deploy previous worker image and keep workflow compatibility.
