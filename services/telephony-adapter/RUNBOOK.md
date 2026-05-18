# telephony-adapter Runbook

Purpose: place outbound calls, handle provider webhooks, route providers, and enforce demo mock calling.

Dependencies: Plivo/Exotel/Twilio or mock provider, FreeSWITCH bridge, consent-compliance, NATS.

Paging signals: call placement failure, webhook signature rejection spike, provider failure rate, idempotency miss.

Common fixes: switch routing to mock/demo, verify provider credentials, inspect webhook signature secret, pause campaigns.

Dashboards and logs: provider selection, call states, failures, webhook replay/idempotency.

Rollback: deploy previous image and force demo/mock mode for smoke tests.
