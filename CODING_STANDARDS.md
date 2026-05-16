# Coding Standards

Derived from the actual code patterns in `libs/go/`, `libs/python/evs_common/`, and `proto/` as built in Phase 1.

## Go

**Module path:** `github.com/lead/libs/go` (see `libs/go/go.mod`).

**Package naming:** lowercase single word matching the directory name (e.g. `package logger`, `package errors`). The one exception is test files which use `package <name>_test` to enforce the public API surface.

**Imports:** stdlib first, then external, then internal — separated by blank lines. Alias local packages only when names collide (e.g. `evsotel`, `evsredis`).

**Error handling:**
- Return `*errors.DomainError` from domain functions; wrap with `errors.Wrap`.
- Never swallow errors silently; always propagate or log.
- Convert to gRPC status at the transport boundary using `errors.ToGRPC(err)`.

**Context propagation:** Every function that performs I/O accepts `context.Context` as its first parameter. Logger and TenantContext are stored in the context via `logger.WithContext` and `auth.WithTenantContext`.

**Logging:** Use `logger.FromContext(ctx)` inside functions; call `logger.New("service-name")` once at startup and inject via `logger.WithContext`.

**Testing:** Table-driven tests preferred for >3 cases. Files end in `_test.go` in the `<pkg>_test` package. No real external connections in unit tests — constructors/struct literals are tested, not live infra calls.

**File layout per package:**
```
<pkg>/
  <pkg>.go        # public API
  <pkg>_test.go   # external black-box tests
```

## Python

**Package structure:** Each service/lib is a standalone uv project with its own `pyproject.toml` registered in the root `pyproject.toml` `[tool.uv.workspace]`.

**Imports:** `from __future__ import annotations` at the top of every module for PEP 563 deferred evaluation. Stdlib → third-party → local.

**Typing:** Full type annotations on all public function signatures. `pydantic.BaseModel` for serialisable data structures.

**Error handling:** Raise `evs_common.errors.DomainError` with the appropriate `Code`. Never catch and silently drop exceptions.

**Async:** All I/O-bound code is `async def`. Tests use `pytest-asyncio` with `asyncio_mode = "auto"`.

**Logging:** Call `evs_common.logger.configure("service-name")` at startup; use `evs_common.logger.get(__name__)` to obtain bound loggers. OTel trace/span IDs are injected automatically.

**Testing:** `pytest` under `tests/`. Unit tests avoid live infra; use constructor-level checks and mock adapters.

## Protobuf / gRPC

**File location:** `proto/evs/v1/<service>.proto` for service definitions; `proto/evs/events/v1/envelope.proto` for the event envelope.

**Package:** `evs.v1` for services; `evs.events.v1` for events.

**Go option:** `option go_package = "github.com/lead/libs/go/pb/evs/v1;evsv1";`

**Naming conventions:**
- Request/response messages named `<Method>Request` / `<Method>Response`.
- Enums prefixed with the type name: `TENANT_STATUS_ACTIVE`.
- All NATS messages are wrapped in `evs.events.v1.Envelope`.

**Codegen:** Run `make proto` (calls `buf generate`). Output lands in `libs/go/pb`, `libs/python/pb`, `libs/ts/pb`. Never hand-edit generated files.

## TypeScript

**Workspace packages** live under `libs/ts/` and `apps/`. Each has a `package.json` with `"private": true`.

**Generated code** in `libs/ts/pb/` is buf output — do not edit.

**Build tool:** Turborepo (`turbo run build`) with pnpm workspaces.

## Local Dev

Run the full infra stack with:
```
docker compose -f infra/docker-compose.dev.yml up -d
```
Or with Tilt for live-reload:
```
tilt up
```

Port map: see `PORT_MAP.md`. Secrets: copy `.env.template` → `.env` and fill in values (never commit `.env`).
