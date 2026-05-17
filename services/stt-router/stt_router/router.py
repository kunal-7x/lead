from __future__ import annotations

import asyncio
import time
from typing import Sequence

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult, EngineHealth
from stt_router.switcher import EngineSwitcher

_TIMEOUT_S = 1.5        # mark engine unhealthy after 1.5s
_UNHEALTHY_TTL_S = 60   # stay unhealthy for 60s
_MIN_CONFIDENCE = 0.55  # re-run on next engine if confidence < this


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
        if lang in ("hi", "hi-en"):
            return ["sarvam", "indicconformer", "groq_whisper", "faster_whisper"]
        # English-first chain
        return ["faster_whisper", "groq_whisper", "sarvam", "indicconformer"]

    async def transcribe(
        self, audio: bytes, lang: str, session_id: str, tenant_id: str
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
                result = await asyncio.wait_for(
                    engine.transcribe(audio, lang, session_id),
                    timeout=_TIMEOUT_S,
                )
            except asyncio.TimeoutError:
                self._mark_unhealthy(name)
                continue
            except Exception:
                self._mark_unhealthy(name)
                continue

            last_result = result

            # Re-run on next engine if confidence is too low
            if result.confidence >= _MIN_CONFIDENCE:
                return result
            # confidence too low — try next engine, keep best result

        if last_result is not None:
            return last_result

        raise RuntimeError("All STT engines failed or unavailable")

    async def engine_health(self) -> list[EngineHealth]:
        results = []
        for name, engine in self._engines.items():
            try:
                ok = await asyncio.wait_for(engine.health_check(), timeout=3.0)
            except Exception:
                ok = False
            results.append(EngineHealth(
                name=name,
                available=ok and self._is_healthy(name),
            ))
        return results
