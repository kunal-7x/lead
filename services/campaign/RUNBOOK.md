# campaign Runbook

Purpose: manage campaign configuration, launch gates, and campaign lifecycle.

Dependencies: tenant-auth, knowledge, scheduler, consent-compliance, billing-meter.

Paging signals: launch failures, invalid RERA or KB gate bypass attempts, campaign state drift.

Common fixes: check approved KB version, verify RERA number, confirm 140-series CLI and calling windows, retry scheduler handoff.

Dashboards and logs: campaign launches, blocked launches, active campaign count, audit events.

Rollback: pause affected campaigns and deploy previous campaign service image.
