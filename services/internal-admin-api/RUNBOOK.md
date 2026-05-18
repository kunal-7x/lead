# internal-admin-api Runbook

Purpose: internal operations API for tenant ops, provider health, audit, flags, kill switches, and support search.

Dependencies: audit store, provider health, tenant records, feature flags, failed jobs.

Paging signals: unauthorized access, missing audit rows, kill switch failure, support search errors.

Common fixes: verify actor role headers, require ticket and reason, inspect audit log, use provider pause controls.

Dashboards and logs: admin actions, forbidden attempts, kill switch events, audit write latency.

Rollback: deploy previous image and restrict access to super_admin until stable.
