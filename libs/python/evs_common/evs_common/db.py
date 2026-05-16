"""Async Postgres helpers with RLS tenant isolation."""
from __future__ import annotations

import asyncpg


async def create_pool(dsn: str, **kwargs) -> asyncpg.Pool:
    """Create an asyncpg connection pool."""
    return await asyncpg.create_pool(dsn, **kwargs)


async def set_tenant_rls(conn: asyncpg.Connection, tenant_id: str) -> None:
    """Set app.tenant_id session variable for Row-Level Security."""
    await conn.execute("SET LOCAL app.tenant_id = $1", tenant_id)


async def write_outbox(
    conn: asyncpg.Connection,
    event_type: str,
    tenant_id: str,
    payload: bytes,
) -> None:
    """Insert a transactional outbox row."""
    await conn.execute(
        """INSERT INTO outbox (event_type, tenant_id, payload, created_at)
           VALUES ($1, $2, $3, NOW())""",
        event_type,
        tenant_id,
        payload,
    )
