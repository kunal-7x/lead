from __future__ import annotations

import asyncio
import json
import os
import time
from typing import AsyncIterator

from llm_router.backends.base import LLMBackend
from llm_router.claim_control import ClaimControl
from llm_router.kb_client import KbRetriever
from llm_router.models import BrainOutput, EngineHealth, LLMRequest, LLMResponse, FALLBACK_BRAIN
from llm_router.switcher import ModelSwitcher

try:
    from evs_common.langfuse_tracer import trace_span
except Exception:  # evs_common optional outside repo
    import contextlib

    @contextlib.asynccontextmanager
    async def trace_span(*a, **kw):  # type: ignore
        yield {"output": None}

_TIMEOUT_S = 20.0

# OpenRouter / LLM provider configuration
# Override via env vars to switch providers without code changes:
#   LLM_BASE_URL  — defaults to OpenRouter
#   LLM_MODEL     — defaults to google/gemini-2.0-flash-001
#   LLM_API_KEY   — falls back to OPENROUTER_API_KEY, then GROQ_API_KEY
_LLM_BASE_URL = os.getenv("LLM_BASE_URL", "https://openrouter.ai/api/v1/chat/completions")
_LLM_MODEL = os.getenv("LLM_MODEL", "google/gemini-2.0-flash-001")
_LLM_API_KEY = (
    os.getenv("LLM_API_KEY")
    or os.getenv("OPENROUTER_API_KEY")
    or os.getenv("GROQ_API_KEY", "")
)


class LLMRouter:
    """Routes LLM requests through backends with RAG, schema enforcement, and failover.

    Flow per request:
    1. Read active model from Redis (per-tenant override → global → default groq_llama)
    2. Retrieve KB chunks from knowledge service (always, before LLM call)
    3. Call active backend with KB context
    4. Validate BrainOutput schema
    5. On failure/timeout → try next available backend
    6. All fail → return FALLBACK_BRAIN (needs_human_review)
    """

    def __init__(
        self,
        backends: dict[str, LLMBackend],
        switcher: ModelSwitcher,
        kb: KbRetriever,
        claim_control: ClaimControl | None = None,
    ) -> None:
        self._backends = backends
        self._switcher = switcher
        self._kb = kb
        self._claim_control = claim_control

    async def generate(self, req: LLMRequest, trace_id: str | None = None) -> LLMResponse:
        t0 = time.time()

        # Always retrieve KB before LLM call
        chunks = await self._kb.retrieve(req.project_id, req.user_turn, req.lang)
        req = req.model_copy(update={"kb_chunks": chunks})
        kb_context = _format_kb(chunks)

        active = await self._switcher.active_model(req.tenant_id)
        chain = _build_chain(active, list(self._backends.keys()))

        last_err: Exception | None = None
        for model_name in chain:
            backend = self._backends.get(model_name)
            if backend is None:
                continue
            try:
                async with trace_span(
                    f"llm.{model_name}",
                    trace_id=trace_id,
                    input={"user_turn": req.user_turn, "lang": req.lang},
                    metadata={"tenant_id": req.tenant_id, "session_id": req.session_id},
                ) as span:
                    brain, pt, ct = await asyncio.wait_for(
                        backend.generate(req, kb_context), timeout=_TIMEOUT_S
                    )
                    span["output"] = {"reply": brain.reply, "lead_score": brain.lead_score,
                                      "next_action": brain.next_action}
                brain = await self._apply_claim_control(req, brain)
                return LLMResponse(
                    brain=brain,
                    model_used=model_name,
                    prompt_tokens=pt,
                    completion_tokens=ct,
                    latency_ms=int((time.time() - t0) * 1000),
                )
            except Exception as exc:
                last_err = exc
                continue

        # All backends failed — safe fallback
        return LLMResponse(
            brain=FALLBACK_BRAIN,
            model_used="fallback",
            latency_ms=int((time.time() - t0) * 1000),
        )

    async def generate_stream(
        self, req: LLMRequest, trace_id: str | None = None
    ) -> AsyncIterator[str]:
        """Stream LLM tokens as SSE-formatted strings.

        Yields SSE lines:
          data: {"token":"<piece>","done":false}\\n\\n
        Final line:
          data: {"token":"","done":true,"summary":"<>","next_action":"<>"}\\n\\n

        If the active backend does not support streaming, falls back to the
        batch generate path and emits the full reply as a single token chunk
        before the final event, so callers always get a valid SSE stream.
        """
        # Retrieve KB, same as batch path
        try:
            chunks = await self._kb.retrieve(req.project_id, req.user_turn, req.lang)
        except Exception:
            chunks = []
        req = req.model_copy(update={"kb_chunks": chunks})
        kb_context = _format_kb(chunks)

        try:
            active = await self._switcher.active_model(req.tenant_id)
        except Exception:
            from llm_router.switcher import DEFAULT_MODEL
            active = DEFAULT_MODEL
        chain = _build_chain(active, list(self._backends.keys()))

        # Try streaming-capable backend first (openrouter/groq_llama both support SSE streaming)
        stream_backend = None
        for model_name in chain:
            backend = self._backends.get(model_name)
            if backend is not None and backend.name in ("openrouter", "groq_llama"):
                stream_backend = (model_name, backend)
                break

        if stream_backend is not None:
            model_name, backend = stream_backend
            try:
                async for sse_line in _llm_stream(backend, req, kb_context):
                    yield sse_line
                return
            except Exception:
                pass  # fall through to batch fallback

        # Fallback: run batch generate on active chain, stream reply as one chunk
        try:
            result = await asyncio.wait_for(
                self.generate(req, trace_id), timeout=_TIMEOUT_S
            )
            brain = result.brain
        except Exception:
            brain = FALLBACK_BRAIN

        # Emit the reply text token-by-token (word-level split for smooth streaming)
        words = brain.reply.split(" ")
        for i, word in enumerate(words):
            piece = word if i == len(words) - 1 else word + " "
            yield f"data: {json.dumps({'token': piece, 'done': False})}\n\n"

        # Final event
        final = {"token": "", "done": True, "summary": brain.summary, "next_action": brain.next_action}
        yield f"data: {json.dumps(final)}\n\n"

    async def _apply_claim_control(self, req: LLMRequest, brain: BrainOutput) -> BrainOutput:
        if self._claim_control is None or not req.project_id:
            return brain
        result = await self._claim_control.check(
            tenant_id=req.tenant_id,
            project_id=req.project_id,
            session_id=req.session_id,
            channel="voice",
            text=brain.reply,
        )
        if result.ok:
            return brain
        reply = result.rewritten or "Let me check that and get back to you."
        reason = "; ".join(v.reason for v in result.violations) or "claim control blocked reply"
        return brain.model_copy(
            update={
                "reply": reply,
                "risk_level": "risky",
                "next_action": "handover",
                "should_handover_to_human": True,
                "summary": f"{brain.summary} Claim-control block: {reason}",
            }
        )

    async def generate_stream_text(
        self, req: LLMRequest, trace_id: str | None = None
    ) -> AsyncIterator[str]:
        """Stream PLAIN TEXT tokens as SSE — no JSON mode, ~0.8s first-token.

        The model is instructed to reply with ONLY the spoken reply (no JSON).
        Yields SSE lines:
          data: {"token":"<piece>","done":false}\\n\\n
        Final line:
          data: {"token":"","done":true}\\n\\n

        This endpoint is for the SPEECH path only. Call the batch /generate
        in parallel to obtain structured metadata (next_action, summary, etc.).

        LATENCY: KB retrieval (embedding + vector search) costs ~2s and would
        block the first token. The parallel structured /generate call already
        does full KB grounding for the record/actions, so the spoken path does
        NOT block on KB — it uses chunks only if the caller already supplied
        them (req.kb_chunks). This drops first-token from ~3s to ~1s.
        """
        chunks = list(getattr(req, "kb_chunks", None) or [])
        kb_context = _format_kb(chunks)

        try:
            active = await self._switcher.active_model(req.tenant_id)
        except Exception:
            from llm_router.switcher import DEFAULT_MODEL
            active = DEFAULT_MODEL
        chain = _build_chain(active, list(self._backends.keys()))

        # Prefer streaming-capable backend for plain-text streaming
        stream_backend = None
        for model_name in chain:
            backend = self._backends.get(model_name)
            if backend is not None and backend.name in ("openrouter", "groq_llama"):
                stream_backend = (model_name, backend)
                break

        if stream_backend is not None:
            model_name, backend = stream_backend
            try:
                async for sse_line in _llm_stream_text(backend, req, kb_context):
                    yield sse_line
                return
            except Exception:
                pass  # fall through to batch fallback

        # Fallback: batch generate, stream reply word-by-word
        try:
            result = await asyncio.wait_for(
                self.generate(req, trace_id), timeout=_TIMEOUT_S
            )
            brain = result.brain
        except Exception:
            brain = FALLBACK_BRAIN

        words = brain.reply.split(" ")
        for i, word in enumerate(words):
            piece = word if i == len(words) - 1 else word + " "
            yield f"data: {json.dumps({'token': piece, 'done': False})}\n\n"
        yield f"data: {json.dumps({'token': '', 'done': True})}\n\n"

    async def engine_health(self) -> list[EngineHealth]:
        results = []
        for name, backend in self._backends.items():
            try:
                ok = await asyncio.wait_for(backend.health_check(), timeout=5.0)
            except Exception:
                ok = False
            results.append(EngineHealth(name=name, available=ok))
        return results


def _format_kb(chunks) -> str:
    if not chunks:
        return "(no KB context)"
    return "\n".join(f"- {c.text}" for c in chunks)


def _build_chain(active: str, available: list[str]) -> list[str]:
    """Active model first, then remaining in order."""
    rest = [n for n in available if n != active]
    return [active] + rest


def _extract_reply_progress(buf: str):
    """Incrementally decode the JSON "reply" string value from a partial buffer.

    Returns (decoded_reply_so_far, complete). decoded_reply_so_far is None until
    the reply value's opening quote appears. Trailing incomplete escape sequences
    (a lone backslash or a partial \\uXXXX) are deferred — they are not emitted
    until enough characters arrive, so we never stream half an escape.
    """
    key = '"reply"'
    i = buf.find(key)
    if i == -1:
        return None, False
    j = buf.find(":", i + len(key))
    if j == -1:
        return None, False
    k = j + 1
    while k < len(buf) and buf[k] in " \t\r\n":
        k += 1
    if k >= len(buf) or buf[k] != '"':
        return None, False
    k += 1  # first char of the value
    out = []
    idx = k
    n = len(buf)
    while idx < n:
        c = buf[idx]
        if c == "\\":
            if idx + 1 >= n:
                break  # incomplete escape — wait for more
            nxt = buf[idx + 1]
            if nxt == "u":
                if idx + 6 > n:
                    break  # incomplete \uXXXX — wait
                try:
                    out.append(chr(int(buf[idx + 2: idx + 6], 16)))
                except ValueError:
                    out.append(buf[idx + 1])
                idx += 6
                continue
            out.append({"n": "\n", "t": "\t", "r": "\r", "b": "\b", "f": "\f",
                        '"': '"', "\\": "\\", "/": "/"}.get(nxt, nxt))
            idx += 2
            continue
        if c == '"':
            return "".join(out), True  # closing quote → reply complete
        out.append(c)
        idx += 1
    return "".join(out), False  # partial


async def _llm_stream(backend, req: LLMRequest, kb_context: str) -> AsyncIterator[str]:
    """Stream JSON-structured tokens from an OpenAI-compatible endpoint (OpenRouter or Groq).

    Builds the same prompt as the batch backend, calls the endpoint with stream=True,
    accumulates the JSON reply across chunks, then validates + emits the final event.

    Provider is selected via module-level _LLM_BASE_URL / _LLM_MODEL / _LLM_API_KEY,
    defaulting to OpenRouter with google/gemini-2.0-flash-001.
    """
    import httpx

    api_key = getattr(backend, "_api_key", None) or _LLM_API_KEY
    if not api_key:
        raise RuntimeError("No LLM API key configured (LLM_API_KEY / OPENROUTER_API_KEY / GROQ_API_KEY)")

    # Resolve model: backend-specific override → env → default (OpenRouter gemini)
    model = getattr(backend, "_model", None) or _LLM_MODEL
    base_url = _LLM_BASE_URL

    messages = backend._build_messages(req, kb_context)
    payload = {
        "model": model,
        "messages": messages,
        "response_format": {"type": "json_object"},
        "temperature": 0.3,
        "stream": True,
    }
    headers = {"Authorization": f"Bearer {api_key}"}

    full_content = ""
    emitted_len = 0       # chars of the decoded reply value already streamed
    reply_done = False    # closing quote of the reply value seen
    async with httpx.AsyncClient(timeout=30.0) as client:
        async with client.stream(
            "POST",
            base_url,
            json=payload,
            headers=headers,
        ) as resp:
            resp.raise_for_status()
            async for line in resp.aiter_lines():
                if not line or not line.startswith("data:"):
                    continue
                raw = line[len("data:"):].strip()
                if raw == "[DONE]":
                    break
                try:
                    chunk = json.loads(raw)
                except Exception:
                    continue
                delta = chunk.get("choices", [{}])[0].get("delta", {})
                token = delta.get("content", "")
                if not token:
                    continue
                full_content += token
                # The model emits a JSON object; stream ONLY the natural-language
                # "reply" string value (decoded), so the worker speaks words, not JSON.
                if not reply_done:
                    decoded, complete = _extract_reply_progress(full_content)
                    if decoded is not None and len(decoded) > emitted_len:
                        piece = decoded[emitted_len:]
                        emitted_len = len(decoded)
                        yield f"data: {json.dumps({'token': piece, 'done': False})}\n\n"
                    if complete:
                        reply_done = True
                        # rest of the JSON (metadata) is accumulated silently

    # Validate the accumulated JSON and emit final event
    try:
        brain = BrainOutput.model_validate_json(full_content)
    except Exception:
        brain = FALLBACK_BRAIN

    final = {"token": "", "done": True, "summary": brain.summary, "next_action": brain.next_action}
    yield f"data: {json.dumps(final)}\n\n"


async def _llm_stream_text(backend, req: LLMRequest, kb_context: str) -> AsyncIterator[str]:
    """Stream PLAIN TEXT tokens from an OpenAI-compatible endpoint — no json_object mode.

    The system prompt instructs the model to respond with ONLY the spoken reply
    (no JSON wrapper). Tokens are streamed as-is. Final SSE event has done=true.

    Provider is selected via module-level _LLM_BASE_URL / _LLM_MODEL / _LLM_API_KEY,
    defaulting to OpenRouter with google/gemini-2.0-flash-001.

    This is the SPEECH path. Metadata (next_action, summary) comes from the
    parallel structured batch call in the worker.
    """
    import httpx
    import sys

    api_key = getattr(backend, "_api_key", None) or _LLM_API_KEY
    if not api_key:
        raise RuntimeError("No LLM API key configured (LLM_API_KEY / OPENROUTER_API_KEY / GROQ_API_KEY)")

    if not hasattr(backend, "_build_messages"):
        raise RuntimeError("Backend does not support _build_messages")

    # Resolve model + URL from env (allows runtime switch from OpenRouter → Groq etc.)
    model = getattr(backend, "_model", None) or _LLM_MODEL
    base_url = _LLM_BASE_URL

    # Build a trimmed system prompt: same persona + KB, but instruct plain-text reply only
    system_lines = [
        f"You are Capsy, an AI real-estate sales assistant for Axcrio.",
        f"Language: {req.lang} (use Hinglish for hi-en).",
        f"KB Context:\n{kb_context}",
        "",
        "INSTRUCTIONS: Respond with ONLY the agent's spoken reply — plain text, "
        "no JSON, no markdown, no extra commentary. "
        "Keep it ≤2 natural sentences, conversational Hinglish.",
    ]
    system = "\n".join(system_lines)

    messages: list[dict] = [{"role": "system", "content": system}]
    for turn in req.dialog_history[-6:]:
        messages.append(turn)
    messages.append({"role": "user", "content": req.user_turn})

    payload = {
        "model": model,
        "messages": messages,
        # NO response_format: json_object — plain text, real streaming from token 1
        "temperature": 0.3,
        "stream": True,
        "max_tokens": 120,  # spoken reply is short; cap to avoid over-generation
    }
    headers = {"Authorization": f"Bearer {api_key}"}

    t0 = time.time()
    first_token_emitted = False

    async with httpx.AsyncClient(timeout=30.0) as client:
        async with client.stream(
            "POST",
            base_url,
            json=payload,
            headers=headers,
        ) as resp:
            resp.raise_for_status()
            async for line in resp.aiter_lines():
                if not line or not line.startswith("data:"):
                    continue
                raw = line[len("data:"):].strip()
                if raw == "[DONE]":
                    break
                try:
                    chunk = json.loads(raw)
                except Exception:
                    continue
                delta = chunk.get("choices", [{}])[0].get("delta", {})
                token = delta.get("content", "")
                if not token:
                    continue
                if not first_token_emitted:
                    first_token_emitted = True
                    ms = (time.time() - t0) * 1000
                    print(
                        f"[llm-router] stream_text first_token ms={ms:.0f}",
                        file=sys.stderr, flush=True,
                    )
                yield f"data: {json.dumps({'token': token, 'done': False})}\n\n"

    yield f"data: {json.dumps({'token': '', 'done': True})}\n\n"
