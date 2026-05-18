# llm-router Runbook

Purpose: route RAG prompts to configured LLM backends and enforce brain JSON schema.

Dependencies: model-config Redis keys, knowledge service, LLM providers, guardrail, Langfuse.

Paging signals: schema mismatch, model timeout, high fallback rate, token cost spike.

Common fixes: switch global model to `groq_llama`, verify provider credentials, inspect prompt version, replay eval harness.

Dashboards and logs: model used, latency, token cost, schema failures, fallback path.

Rollback: pin previous prompt/model and deploy previous router image.
