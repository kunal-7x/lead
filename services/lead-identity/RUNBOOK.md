# lead-identity Runbook

Purpose: resolve, dedupe, and update lead identities across channels.

Dependencies: Postgres, lead import, WhatsApp, telephony, consent ledger.

Paging signals: duplicate lead spike, wrong tenant mapping, identity merge conflict, status update failures.

Common fixes: inspect dedupe keys, replay import batch, reverse bad merge from audit trail, pause affected imports.

Dashboards and logs: lead create/update rate, duplicate rate, merge actions, tenant isolation checks.

Rollback: deploy previous image and disable automatic merges.
