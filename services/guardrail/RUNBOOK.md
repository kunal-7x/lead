# guardrail Runbook

Purpose: validate and rewrite LLM brain JSON before customer-facing TTS.

Dependencies: llm-router output, KB chunks, hallucination incident storage, Langfuse tracing.

Paging signals: unsafe output leakage, hallucination incidents spike, prompt injection bypass, schema errors.

Common fixes: force handoff mode, roll back prompt version, inspect KB chunks, add golden regression cases.

Dashboards and logs: guardrail decisions, risky/unsafe rate, hallucination incidents, prompt versions.

Rollback: deploy previous guardrail image and activate conservative handoff policy.
