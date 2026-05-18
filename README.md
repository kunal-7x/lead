# Capsy EVS

Capsy EVS is an AI-assisted real-estate voice, WhatsApp, lead, billing, analytics, and internal-ops platform. The system is built as a monorepo with Go control-plane services, Python AI-plane services, Next.js apps, protobuf contracts, and deployment scaffolding.

## Quickstart

Prerequisites:

- Go 1.24+
- Python 3.12 with `uv`
- Node.js with Corepack and pnpm 9
- Docker Desktop for local infra
- Optional: Tilt/Skaffold, buf, gitleaks, trivy, syft, grype, ZAP

Common local commands:

```bash
corepack pnpm install
uv sync
go test ./tests/security
cd apps/dashboard && corepack pnpm exec next build
cd scripts && python seed_demo.py --dry-run
```

Local cluster outline:

```bash
docker compose up -d postgres redis nats temporal clickhouse
tilt up
```

If Tilt is unavailable, run individual services with their runbooks under `services/<name>/RUNBOOK.md`.

## Production Deploy Outline

1. Build service images in CI.
2. Run protobuf, OpenAPI, unit, integration, security, and e2e gates.
3. Publish images to the registry.
4. Apply Terraform for DOKS, databases, Vault, Cloudflare, B2/Spaces, observability, and secrets.
5. Deploy with Helm or Kustomize.
6. Run smoke tests against BFF, dashboard, internal admin, telephony webhooks, WhatsApp webhooks, and AI routers.

Rollback:

- Revert the Helm release to the previous image tag.
- Re-run migrations only when marked reversible.
- Use provider kill switches for AI/telephony/WA incidents.

## Link Map

- Architecture: `ARCHITECTURE.md`
- Final build summary: `DONE.md`
- API docs: `docs/api/README.md`
- Engineer onboarding: `docs/onboarding/engineer.md`
- Client onboarding: `docs/onboarding/client.md`
- Sales onboarding: `docs/onboarding/sales.md`
- Coding standards: `CODING_STANDARDS.md`
- Dependency adoption: `DEPENDENCIES.md`
- Security audit: `../phases/phase-24/AUDIT_REPORT.md`
