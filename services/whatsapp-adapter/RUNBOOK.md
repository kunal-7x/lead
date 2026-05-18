# whatsapp-adapter Runbook

Purpose: send WhatsApp templates/messages/flows, process webhooks, enforce opt-out and demo mock sends.

Dependencies: Meta Cloud API or demo client, consent-compliance, templates, billing-meter, NATS.

Paging signals: send failure, webhook signature failures, opt-out miss, template sync errors, rate limit retry backlog.

Common fixes: verify Vault credentials, replay retry queue, sync templates, force demo client for sandbox tenant, pause marketing sends.

Dashboards and logs: send volume, delivery/read status, opt-outs, template status, retry tasks.

Rollback: deploy previous image and disable outbound WA sends for affected tenants.
