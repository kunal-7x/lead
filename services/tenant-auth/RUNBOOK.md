# tenant-auth Runbook

Purpose: tenants, users, roles, JWT, audit, and tenant isolation.

Dependencies: Postgres, Vault/JWT secret, RBAC policy, dashboard and BFF.

Paging signals: login failure spike, cross-tenant access attempt, JWT validation error, RLS policy failure.

Common fixes: verify JWT secret, inspect RLS tests, revoke compromised sessions, restore role assignments from audit.

Dashboards and logs: auth success/failure, forbidden attempts, tenant create/update, audit writes.

Rollback: deploy previous image and temporarily disable risky admin mutations.
