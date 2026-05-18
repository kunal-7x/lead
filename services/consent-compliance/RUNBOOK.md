# consent-compliance Runbook

Purpose: enforce outreach consent, opt-out, DPDP, TRAI, and contact-window rules.

Dependencies: consent ledger, lead records, campaign settings, audit log.

Paging signals: outreach without ledger entry, opt-out miss, blocked campaign bypass, export or erasure SLA miss.

Common fixes: stop affected campaign, replay ledger writes, verify opt-out sync, run subject-access export verification.

Dashboards and logs: consent decisions, block reasons, erasure jobs, export signatures.

Rollback: deploy previous image and default to deny outreach while investigating.
