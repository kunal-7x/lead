# bff Runbook

Purpose: external backend-for-frontend API for dashboard workflows.

Dependencies: tenant-auth, lead, campaign, billing, reporting, and admin-facing services.

Paging signals: 5xx rate, auth failures, request latency, upstream timeout rate.

Common fixes: verify upstream service health, check JWT secret and tenant context, inspect recent route changes, roll back the BFF image.

Dashboards and logs: HTTP latency, status code mix, upstream error budget, structured request logs.

Rollback: deploy previous BFF image and keep database migrations backward compatible.
