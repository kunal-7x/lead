# Demo-First Completion Phases

These phases extend the Phase 0-25 scaffold. They intentionally finish the product in fake/demo mode before production conversion.

| Phase | Name | Outcome |
|---|---|---|
| 26 | Demo acceptance harness | Defines and tests the required 10-lead journey. |
| 27 | BFF product proxy | Dashboard APIs reach the existing product services. |
| 28 | Shared demo state | Services share deterministic demo state instead of isolated runtime fakes. |
| 29 | Campaign to call orchestration | Imported leads can be attached, scheduled, and fake-called. |
| 30 | Voice event persistence | Demo voice turns, transcripts, brain outputs, summaries, and actions persist. |
| 31 | WhatsApp demo brain loop | Follow-ups, replies, inbox, and timeline work through APIs. |
| 32 | Dashboard proof surfaces | Client sees all proof: calls, transcripts, WA, site visits, handoffs, reports, cost. |
| 33 | Full demo E2E pass | One command proves the complete product journey. |
| 34 | Production readiness gate | Runtime fakes are forbidden in production mode and remaining prod work is explicit. |

After Phase 34, production conversion can start with real Postgres stores, real event bus, real provider credentials, staging deployment, and small live-call validation.

