"""Langfuse tracing wrapper (langfuse v4 SDK).

Single entry point for all services. Falls back to no-op when
LANGFUSE_PUBLIC_KEY/SECRET_KEY are unset.

Usage:
    from evs_common.langfuse_tracer import trace_span, propagate_trace_id

    trace_id = propagate_trace_id(request.headers)
    async with trace_span("llm.groq", trace_id=trace_id, input={"q": q}) as s:
        brain = await backend.generate(...)
        s["output"] = brain.reply

Headers: every outbound HTTP call should include X-Trace-Id so STT → LLM →
TTS share one trace. Use `propagate_trace_id()` to read or mint one.
"""
from __future__ import annotations

import contextlib
import os
import uuid
from typing import Any, AsyncIterator

_CLIENT: Any = None
_INIT_TRIED = False


def get_client():
    """Return Langfuse client or None. Idempotent — safe to call per-request."""
    global _CLIENT, _INIT_TRIED
    if _INIT_TRIED:
        return _CLIENT
    _INIT_TRIED = True
    pk = os.getenv("LANGFUSE_PUBLIC_KEY", "")
    sk = os.getenv("LANGFUSE_SECRET_KEY", "")
    host = os.getenv("LANGFUSE_HOST", "https://cloud.langfuse.com")
    if not (pk and sk):
        return None
    try:
        from langfuse import Langfuse  # type: ignore
        _CLIENT = Langfuse(public_key=pk, secret_key=sk, host=host)
        return _CLIENT
    except Exception:
        _CLIENT = None
        return None


def propagate_trace_id(headers: dict[str, str] | None) -> str:
    """Return X-Trace-Id from headers or mint a new 32-char hex id."""
    if headers:
        tid = headers.get("X-Trace-Id") or headers.get("x-trace-id")
        if tid:
            return tid
    return uuid.uuid4().hex


@contextlib.asynccontextmanager
async def trace_span(
    name: str,
    *,
    trace_id: str | None = None,
    kind: str = "generation",
    input: Any = None,
    metadata: dict | None = None,
) -> AsyncIterator[dict]:
    """Emit one Langfuse observation. Yields a dict — set span["output"] to
    capture the result. Errors are recorded then re-raised.
    """
    client = get_client()
    span_ref: dict = {"output": None, "error": None}

    if client is None:
        yield span_ref
        return

    as_type = "generation" if kind == "generation" else "span"
    obs = None
    try:
        obs = client.start_observation(
            name=name,
            as_type=as_type,
            input=input,
            metadata={**(metadata or {}), "trace_id_hint": trace_id} if trace_id else metadata,
        )
    except Exception:
        # Tracing must never break the call path.
        yield span_ref
        return

    try:
        yield span_ref
        try:
            obs.update(output=span_ref.get("output"))
        except Exception:
            pass
    except Exception as e:
        span_ref["error"] = str(e)
        try:
            obs.update(output=None, level="ERROR", status_message=str(e))
        except Exception:
            pass
        raise
    finally:
        try:
            obs.end()
        except Exception:
            pass
        try:
            client.flush()
        except Exception:
            pass
