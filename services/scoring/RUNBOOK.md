# scoring Runbook

Purpose: score leads and route prioritization signals.

Dependencies: lead features, model artifact, analytics events, campaign configuration.

Paging signals: scoring latency, model load failure, unexpected score distribution, feature missing spike.

Common fixes: roll back model artifact, verify feature extraction, use rules fallback, replay scoring batch.

Dashboards and logs: score distribution, model version, feature missing rate, hot lead conversion.

Rollback: deploy previous model artifact and service image.
