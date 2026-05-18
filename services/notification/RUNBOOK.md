# notification Runbook

Purpose: deliver email, Slack, Discord, and internal notification events.

Dependencies: Postmark/SES, Slack/Discord webhooks, templates, NATS events.

Paging signals: notification send failures, provider rate limit, template render errors, queue backlog.

Common fixes: verify provider credentials, switch channel fallback, replay failed sends, pause noisy tenant.

Dashboards and logs: send rate, failure rate, provider latency, retry queue depth.

Rollback: deploy previous image and disable non-critical notification types.
