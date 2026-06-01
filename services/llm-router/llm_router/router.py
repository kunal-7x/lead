from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from typing import AsyncIterator

import httpx

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

logger = logging.getLogger("llm_router.router")

_TIMEOUT_S = 20.0

# Concurrency guard: max simultaneous provider calls across all in-flight requests.
_MAX_CONCURRENT = int(os.getenv("LLM_MAX_CONCURRENT", "8"))
_semaphore: asyncio.Semaphore | None = None


def _get_semaphore() -> asyncio.Semaphore:
    global _semaphore
    if _semaphore is None:
        _semaphore = asyncio.Semaphore(_MAX_CONCURRENT)
    return _semaphore


# Backoff schedule for 429 / 5xx responses from provider.
_BACKOFF_SCHEDULE = (0.25, 0.5, 1.0)

# Module-level singleton httpx client for streaming LLM calls.
# Created lazily so it lives inside the event loop (no creation at import time).
_http_client: httpx.AsyncClient | None = None


def _get_http_client() -> httpx.AsyncClient:
    global _http_client
    if _http_client is None:
        _http_client = httpx.AsyncClient(
            timeout=30.0,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )
    return _http_client

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
                    brain, pt, ct = await self._call_with_backoff(
                        backend, req, kb_context
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

    async def _call_with_backoff(
        self,
        backend: LLMBackend,
        req: LLMRequest,
        kb_context: str,
    ):
        """Acquire the concurrency semaphore then call backend.generate().

        Retries with exponential backoff on HTTP 429 / 5xx errors.
        """
        sem = _get_semaphore()
        last_exc: Exception | None = None
        async with sem:
            for attempt, backoff in enumerate(_BACKOFF_SCHEDULE + (None,)):  # type: ignore[operator]
                try:
                    return await asyncio.wait_for(
                        backend.generate(req, kb_context), timeout=_TIMEOUT_S
                    )
                except asyncio.TimeoutError:
                    raise
                except Exception as exc:
                    msg = str(exc).lower()
                    retriable = (
                        "429" in msg
                        or "rate limit" in msg
                        or "503" in msg
                        or "502" in msg
                        or "500" in msg
                    )
                    if retriable and backoff is not None:
                        await asyncio.sleep(backoff)
                        last_exc = exc
                        continue
                    raise
        raise last_exc  # type: ignore[misc]

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

        # Try streaming-capable backend first (groq_llama/scout primary, cerebras_llama fallback)
        # openrouter removed — 404 errors (misconfigured model slug); 3 Groq keys + Cerebras covers fallback
        stream_backend = None
        for model_name in chain:
            backend = self._backends.get(model_name)
            if backend is not None and backend.name in ("groq_llama", "cerebras_llama"):
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
        # Claim-control is a post-LLM guardrail enrichment, not a hard dependency.
        # If the service is unreachable (DNS failure / timeout / 5xx) we MUST NOT
        # discard a good LLM response — returning the brain unchanged. Previously a
        # claim-control outage raised here, INSIDE generate()'s backend try-block,
        # so a healthy LLM turn was mis-treated as a backend failure → the chain
        # exhausted → FALLBACK_BRAIN. This was the second cause of /v1/llm/generate
        # falling back (only when project_id was set → claim-control invoked).
        try:
            result = await self._claim_control.check(
                tenant_id=req.tenant_id,
                project_id=req.project_id,
                session_id=req.session_id,
                channel="voice",
                text=brain.reply,
            )
        except Exception as exc:  # noqa: BLE001
            logger.warning(
                "claim_control check failed (%r) — returning LLM brain unchanged", exc
            )
            return brain
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

        # Try each streaming-capable backend in chain order.
        # On 429 or any error, skip that model and advance to the next one.
        # Within groq_llama(scout), 3-key round-robin absorbs 429 before escalating.
        # Chain order: groq_llama(scout) → cerebras_llama.
        # openrouter removed: 404 errors (misconfigured model slug); not needed with 3 Groq keys.
        # groq_instant removed from chain (low token limits).
        _STREAMING_CAPABLE = ("groq_llama", "cerebras_llama")
        import sys as _sys
        brain = None
        for model_name in chain:
            backend = self._backends.get(model_name)
            if backend is None or backend.name not in _STREAMING_CAPABLE:
                continue
            try:
                async for sse_line in _llm_stream_text(backend, req, kb_context):
                    yield sse_line
                return  # success — done
            except Exception as _stream_err:
                print(
                    f"[llm-router] stream_text {model_name} failed — trying next chain member"
                    f" session={req.session_id} lang={req.lang} err={_stream_err!r}",
                    file=_sys.stderr, flush=True,
                )
                # continue to next model in chain (skip this 429'd / failed model)

        # All streaming backends failed — emit FALLBACK as a single word-by-word stream.
        # req.lang is preserved from the original request so Hindi stays Hindi.
        print(
            f"[llm-router] stream_text all streaming backends failed, emitting fallback"
            f" session={req.session_id}",
            file=_sys.stderr, flush=True,
        )
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


_PREFERRED_ORDER = [
    "groq_llama",       # scout (meta-llama/llama-4-scout-17b-16e-instruct) — 3-key round-robin
    "cerebras_llama",   # gpt-oss-120b reasoning_effort=low — batch fallback
    # openrouter removed: was returning 404 (misconfigured model slug); 3 Groq keys + Cerebras covers fallback
]


def _build_chain(active: str, available: list[str]) -> list[str]:
    """Active model first, then remaining in preferred fallback order.

    Preferred order: groq_llama(scout) → cerebras_llama.
    groq_instant and openrouter removed from chain.
    3-key round-robin inside GroqLlamaBackend handles Groq 429 before escalating.
    Models not in the preferred list come last (in their registration order).
    """
    rest_ordered = []
    # First pass: preferred order, excluding active
    for name in _PREFERRED_ORDER:
        if name != active and name in available:
            rest_ordered.append(name)
    # Second pass: anything not in preferred list, in registration order
    preferred_set = set(_PREFERRED_ORDER)
    for name in available:
        if name != active and name not in preferred_set:
            rest_ordered.append(name)
    return [active] + rest_ordered


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
    api_key = getattr(backend, "_api_key", None) or _LLM_API_KEY
    if not api_key:
        raise RuntimeError("No LLM API key configured (LLM_API_KEY / OPENROUTER_API_KEY / GROQ_API_KEY)")

    # Resolve model + URL: backend-specific → env → OpenRouter default
    model = getattr(backend, "_model", None) or _LLM_MODEL
    base_url = getattr(backend, "_url", None) or _LLM_BASE_URL

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
    client = _get_http_client()
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
    import sys

    api_key = getattr(backend, "_api_key", None) or _LLM_API_KEY
    if not api_key:
        raise RuntimeError("No LLM API key configured (LLM_API_KEY / OPENROUTER_API_KEY / GROQ_API_KEY)")

    if not hasattr(backend, "_build_messages"):
        raise RuntimeError("Backend does not support _build_messages")

    # Resolve model + URL: backend-specific → env → OpenRouter default
    model = getattr(backend, "_model", None) or _LLM_MODEL
    base_url = getattr(backend, "_url", None) or _LLM_BASE_URL

    # Build a trimmed system prompt: same persona + KB, but instruct plain-text reply only.
    # Reuse the SHARED persona so the spoken path behaves identically to the batch path.
    # Persona gender comes from the campaign's chosen voice (req.persona_gender), so the
    # spoken path's Hindi verb-gender forms agree with the synthesized voice.
    from llm_router.backends.base import build_persona_prompt, _build_slots_block, format_campaign_context

    slots_block = _build_slots_block(req.collected_slots)
    campaign_block = format_campaign_context(req.campaign_context or {})
    system_lines = [
        build_persona_prompt(getattr(req, "persona_gender", None) or "female"),
        slots_block,
        f"KB Context (अगर ज़रूरी हो तो इसी से जानकारी दो):\n{kb_context}",
        "",
        "अभी सिर्फ़ बोला जाने वाला जवाब दो — कोई JSON नहीं, कोई schema नहीं। "
        "ऊपर दिए SPEECH OUTPUT RULES को सख्ती से follow करो: Devanagari Hindi, "
        "कोई markdown/bullets/symbols नहीं, punctuation से prosody, numbers spoken-form, "
        "max 1 filler। जवाब की लंबाई बातचीत की असली ज़रूरत के हिसाब से — कोई शब्द-सीमा नहीं।",
    ]
    # Inject campaign-specific context (product, offer, talking points, etc.) when present.
    if campaign_block:
        system_lines.append(f"\n{campaign_block}")
    # T2.1: Adaptive per-turn directive (CSO/RSP) — appended as suffix when present.
    if req.system_prompt_suffix:
        system_lines.append(f"\n{req.system_prompt_suffix}")
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
        "max_tokens": 400,  # allow genuine multi-sentence natural spoken replies
    }
    # Backend-specific extra payload (e.g. Cerebras gpt-oss-120b needs
    # reasoning_effort=none so it streams real delta.content, not reasoning).
    extra = getattr(backend, "_stream_extra_payload", None)
    if extra:
        payload.update(extra)
    headers = {"Authorization": f"Bearer {api_key}"}

    t0 = time.time()
    first_token_emitted = False

    _client = _get_http_client()
    async with _client.stream(
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
