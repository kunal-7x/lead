# Engineer Onboarding

## Laptop Setup

Install Go, Python with `uv`, Node.js with Corepack, Docker Desktop, buf, and pnpm.

```bash
corepack pnpm install
uv sync
go test ./tests/security
```

## Local Development

- Use Tilt/Skaffold for full-stack local orchestration when available.
- Use service runbooks for single-service work.
- Keep contracts first: update protobuf/OpenAPI, regenerate clients, then implement.
- Follow `../../CODING_STANDARDS.md`.
- Fill `../../DEPENDENCIES.md` before adopting third-party packages.

## Adding A Service

1. Add the service under `services/<name>`.
2. Add tests and a `RUNBOOK.md`.
3. Add workspace entries (`go.work`, `pyproject.toml`, or pnpm workspace).
4. Add ports to `PORT_MAP.md`.
5. Add docs and e2e coverage when user-facing.
