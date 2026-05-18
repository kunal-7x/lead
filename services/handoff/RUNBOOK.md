# handoff Runbook

Purpose: route hot leads and escalations to humans with SLA tracking.

Dependencies: lead identity, notification, sales assignments, NATS events.

Paging signals: handoff queue backlog, SLA breach, notification send failure, assignment miss.

Common fixes: check salesperson roster, replay handoff events, verify notification providers, manually assign urgent leads.

Dashboards and logs: handoff volume, SLA timers, accepted handoffs, missed notifications.

Rollback: deploy previous image and temporarily route all handoffs to manager queue.
