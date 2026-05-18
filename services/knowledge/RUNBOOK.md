# knowledge Runbook

Purpose: ingest project knowledge, approve KB versions, and serve RAG retrieval.

Dependencies: Postgres, embeddings/vector index, campaign approval flow, object storage.

Paging signals: retrieval latency, failed KB import, unapproved KB used by campaign, embedding errors.

Common fixes: re-run ingestion, verify project RERA metadata, rebuild embeddings, block campaign launch until KB is approved.

Dashboards and logs: KB versions, retrieval hit rate, embedding jobs, approval state changes.

Rollback: pin previous approved KB version and deploy previous service image.
