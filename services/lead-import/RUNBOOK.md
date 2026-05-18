# lead-import Runbook

Purpose: validate and import lead files and source integrations.

Dependencies: object storage, lead-identity, tenant-auth, consent-compliance.

Paging signals: import failure rate, invalid row spike, stuck batch, source connector errors.

Common fixes: check CSV schema, quarantine bad rows, retry idempotent batch, verify tenant/source mapping.

Dashboards and logs: rows imported, rejected rows, batch duration, source health.

Rollback: deploy previous image and pause new import batches.
