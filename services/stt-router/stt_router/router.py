from __future__ import annotations

import asyncio
import os
import time
from typing import Sequence

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult, EngineHealth
from stt_router.switcher import EngineSwitcher

try:
    from evs_common.langfuse_tracer import trace_span
except Exception:
    import contextlib

    @contextlib.asynccontextmanager
    async def trace_span(*a, **kw):  # type: ignore
        yield {"output": None}

# Default per-engine timeout. Cloud STT APIs (sarvam/groq) need a realistic
# round-trip budget — a real utterance measures ~1.5–5s — so 1.5s is fatal.
# Env-overridable via STT_ENGINE_TIMEOUT_S. Self-hosted engines that are known
# to be fast get a tighter timeout via _ENGINE_TIMEOUT_S below.
_TIMEOUT_S = float(os.getenv("STT_ENGINE_TIMEOUT_S", "8.0"))

# Concurrency guard: max simultaneous provider calls across all in-flight requests.
# Prevents serialization/queue build-up under concurrent calls. Env-configurable.
_MAX_CONCURRENT = int(os.getenv("STT_MAX_CONCURRENT", "8"))
_semaphore: asyncio.Semaphore | None = None


def _get_semaphore() -> asyncio.Semaphore:
    """Return (lazily creating) the module-level semaphore.

    Lazy creation is required because asyncio.Semaphore must be created inside
    an active event loop. Under uvicorn this is always the case; tests that
    call transcribe() also have a running loop via pytest-asyncio.
    """
    global _semaphore
    if _semaphore is None:
        _semaphore = asyncio.Semaphore(_MAX_CONCURRENT)
    return _semaphore


# Backoff schedule (seconds) for 429 / 5xx provider responses.
_BACKOFF_SCHEDULE = (0.25, 0.5, 1.0)

# Per-engine timeout overrides. Self-hosted/GPU engines respond fast, so they
# keep a tight budget; cloud engines fall back to the (larger) default.
# Sarvam gets 20s to allow the engine's HTTP/2 retry path to complete:
#   worst case = 8s httpx attempt 1 (ReadTimeout) + <1s client recycle + 8s retry = ~17s.
_ENGINE_TIMEOUT_S = {
    "faster_whisper": 1.5,
    "indicconformer": 1.5,
    "sarvam": float(os.getenv("STT_SARVAM_TIMEOUT_S", "20.0")),
}

_UNHEALTHY_TTL_S = 15   # stay unhealthy for 15s (was 60s — too punishing on a transient timeout)
_MIN_CONFIDENCE = 0.55  # re-run on next engine if confidence < this (only when STT_CONFIDENCE_RERUN=1)
# Confidence-triggered serial re-run doubles latency when Sarvam returns moderate confidence
# (e.g. 0.4–0.54). Disabled by default; enable only for offline/batch use-cases.
_CONFIDENCE_RERUN = os.getenv("STT_CONFIDENCE_RERUN", "0") == "1"
_HEALTH_CACHE_TTL_S = 5.0  # cache health ping results for 5 seconds


def _engine_timeout(name: str) -> float:
    return _ENGINE_TIMEOUT_S.get(name, _TIMEOUT_S)


class STTRouter:
    """Routes STT requests through the engine chain with failover and re-run logic.

    Engine priority (overridden by Redis switcher for admin-selected engine):
      1. sarvam        — Hinglish/Hindi, API-only
      2. indicconformer — Indian languages, self-hosted CPU
      3. faster_whisper — English/multilingual, GPU optional
      4. groq_whisper  — API fallback

    Routing rules:
    - lang=hi or hi-en → sarvam first
    - lang=en → faster_whisper first, then groq_whisper
    - confidence < 0.55 on final → re-run on next engine
    - timeout > 1.5s → mark engine unhealthy for 60s
    """

    def __init__(
        self,
        engines: dict[str, STTEngine],
        switcher: EngineSwitcher,
    ) -> None:
        self._engines = engines
        self._switcher = switcher
        self._unhealthy_until: dict[str, float] = {}
        self._health_cache: dict[str, tuple[bool, float]] = {}  # name → (ok, expires_at)

    def _is_healthy(self, name: str) -> bool:
        until = self._unhealthy_until.get(name, 0)
        return time.time() >= until

    def _mark_unhealthy(self, name: str) -> None:
        self._unhealthy_until[name] = time.time() + _UNHEALTHY_TTL_S

    def _priority_chain(self, lang: str, forced: str | None) -> list[str]:
        if forced and forced in self._engines:
            others = [n for n in self._default_chain(lang) if n != forced]
            return [forced] + others
        return self._default_chain(lang)

    def _default_chain(self, lang: str) -> list[str]:
        # Treat any Hindi/Indian-language locale as Hindi-first.
        # hi-IN is what Sarvam streaming STT reports; normalise alongside hi and hi-en.
        if lang in ("hi", "hi-en", "hi-IN") or lang.startswith("hi"):
            return ["sarvam", "indicconformer", "groq_whisper", "faster_whisper"]
        # English-first chain
        return ["faster_whisper", "groq_whisper", "sarvam", "indicconformer"]

    async def transcribe(
        self, audio: bytes, lang: str, session_id: str, tenant_id: str,
        trace_id: str | None = None,
    ) -> STTResult:
        forced = await self._switcher.active_engine()
        chain = self._priority_chain(lang, forced)

        last_result: STTResult | None = None

        for name in chain:
            engine = self._engines.get(name)
            if engine is None:
                continue
            if not self._is_healthy(name):
                continue

            try:
                async with trace_span(
                    f"stt.{name}",
                    trace_id=trace_id,
                    input={"audio_bytes": len(audio), "lang": lang},
                    metadata={"tenant_id": tenant_id, "session_id": session_id},
                ) as span:
                    result = await self._call_with_backoff(
                        engine, audio, lang, session_id, name
                    )
                    span["output"] = {"text": result.text, "confidence": result.confidence}
            except asyncio.TimeoutError:
                self._mark_unhealthy(name)
                continue
            except Exception:
                self._mark_unhealthy(name)
                continue

            last_result = result

            # Always return immediately on first successful (non-empty) result.
            # Serial confidence-triggered re-run is disabled by default because it
            # doubles latency (two cloud STT calls back-to-back). Enable via env
            # STT_CONFIDENCE_RERUN=1 for offline/batch pipelines that can afford it.
            if not _CONFIDENCE_RERUN or result.confidence >= _MIN_CONFIDENCE:
                return result
            # confidence too low AND rerun enabled — try next engine, keep best result

        if last_result is not None:
            return last_result

        raise RuntimeError("All STT engines failed or unavailable")

    async def _call_with_backoff(
        self,
        engine: STTEngine,
        audio: bytes,
        lang: str,
        session_id: str,
        name: str,
    ) -> STTResult:
        """Acquire the concurrency semaphore then call the engine.

        On HTTP 429 or 5xx, retries with exponential backoff up to 3 attempts
        before re-raising (which causes the router to try the next engine).
        TimeoutError from asyncio.wait_for is re-raised immediately — it is
        handled by the caller.
        """
        sem = _get_semaphore()
        last_exc: Exception | None = None
        async with sem:
            for attempt, backoff in enumerate(_BACKOFF_SCHEDULE + (None,)):  # type: ignore[operator]
                try:
                    return await asyncio.wait_for(
                        engine.transcribe(audio, lang, session_id),
                        timeout=_engine_timeout(name),
                    )
                except asyncio.TimeoutError:
                    raise  # timeout → mark unhealthy; no retry
                except Exception as exc:
                    # Retry on rate-limit / server errors; give up on others.
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
        raise last_exc  # type: ignore[misc]  # unreachable but satisfies type checker

    async def engine_health(self) -> list[EngineHealth]:
        now = time.time()
        results = []
        for name, engine in self._engines.items():
            cached_ok, expires_at = self._health_cache.get(name, (False, 0.0))
            if now < expires_at:
                ok = cached_ok
            else:
                try:
                    ok = await asyncio.wait_for(engine.health_check(), timeout=3.0)
                except Exception:
                    ok = False
                self._health_cache[name] = (ok, now + _HEALTH_CACHE_TTL_S)
            results.append(EngineHealth(
                name=name,
                available=ok and self._is_healthy(name),
            ))
        return results
