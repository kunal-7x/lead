# eval-harness Runbook

Purpose: run deterministic AI evaluation suites and gate prompt promotion.

Dependencies: golden test cases, prompt versions, model versions, AI feedback, review queue.

Paging signals: prompt promotion blocked unexpectedly, deterministic scores drift, feedback corpus write failures.

Common fixes: pin model and seed, inspect high-severity case failures, compare baseline run ids, replay accepted feedback.

Dashboards and logs: prompt run status, high-severity score, blocked promotions, feedback acceptance.

Rollback: keep previous prompt active and revert candidate prompt version.
