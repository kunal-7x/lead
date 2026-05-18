# billing-meter Runbook

Purpose: meter call, WhatsApp, STT, TTS, LLM, and campaign costs.

Dependencies: usage events, pricing tables, tenant caps, credit ledger, signal bus.

Paging signals: cap signals missing, negative balances, duplicate cost events, usage ingestion lag.

Common fixes: verify idempotency keys, replay usage events, adjust caps through admin API, pause campaigns if tenant cap is reached.

Dashboards and logs: usage count, cost total, cap warnings, credit ledger changes.

Rollback: deploy previous image and freeze billing mutations if ledger consistency is at risk.
