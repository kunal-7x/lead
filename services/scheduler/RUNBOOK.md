# scheduler Runbook

Purpose: schedule campaign outreach, retries, callbacks, and workflow timers.

Dependencies: Temporal, NATS, campaign, consent-compliance, billing caps.

Paging signals: workflow backlog, missed schedule, callback SLA miss, Temporal connection errors.

Common fixes: restart workers, inspect Temporal namespace, replay schedule events, pause campaign if caps or compliance gates block.

Dashboards and logs: workflow count, schedule lag, retry rate, callback due queue.

Rollback: deploy previous scheduler and keep existing workflow definitions compatible.
