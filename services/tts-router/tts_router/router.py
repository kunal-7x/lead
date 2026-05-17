from __future__ import annotations

import asyncio
import time

from tts_router.audio import ensure_8khz_l16
from tts_router.cache import AudioCache, cache_key
from tts_router.engines.base import TTSEngine
from tts_router.models import TTSRequest, TTSResult
from tts_router.pronunciation import normalize
from tts_router.switcher import EngineSwitcher

_TIMEOUT_S = 8.0


class TTSRouter:
    """Routes TTS requests through the engine chain.

    Flow:
    1. Pronunciation normalization (Indian numbers, currency, phone, dates)
    2. Cache lookup (sha256 key) — hit returns immediately
    3. Read active engine from Redis (per-tenant → global → sarvam_bulbul)
    4. Premium gate: ElevenLabs only if req.tts_premium=True
    5. Synthesize via active engine; on failure → try next in chain
    6. Resample to 8kHz L16 PCM; store in cache; return
    """

    def __init__(
        self,
        engines: dict[str, TTSEngine],
        switcher: EngineSwitcher,
        cache: AudioCache,
    ) -> None:
        self._engines = engines
        self._switcher = switcher
        self._cache = cache

    async def synthesize(self, req: TTSRequest) -> TTSResult:
        t0 = time.time()

        # Pronunciation normalization
        text = normalize(req.text)

        # Cache check
        key = cache_key(text, req.voice_id, req.lang)
        cached = await self._cache.get(key)
        if cached is not None:
            return TTSResult(
                audio=cached,
                tier_used="cache",
                latency_ms=int((time.time() - t0) * 1000),
                cache_hit=True,
            )

        # Build engine chain
        active = await self._switcher.active_engine(req.tenant_id)
        chain = _build_chain(active, list(self._engines.keys()))

        for engine_name in chain:
            engine = self._engines.get(engine_name)
            if engine is None:
                continue
            # Premium gate
            if engine.premium_only and not req.tts_premium:
                continue
            try:
                raw = await asyncio.wait_for(
                    engine.synthesize(text, req.voice_id, req.lang),
                    timeout=_TIMEOUT_S,
                )
                audio = ensure_8khz_l16(raw)
                await self._cache.put(key, audio)
                return TTSResult(
                    audio=audio,
                    tier_used=engine_name,
                    latency_ms=int((time.time() - t0) * 1000),
                    cache_hit=False,
                )
            except Exception:
                continue

        # All engines failed — return silence
        from tts_router.audio import make_silent_pcm
        return TTSResult(
            audio=make_silent_pcm(500),
            tier_used="silence",
            latency_ms=int((time.time() - t0) * 1000),
        )

    async def warm(self, phrases: list[str], lang: str, voice_id: str, tenant_id: str) -> int:
        """Pre-synthesize phrases and store in cache. Returns number of entries warmed."""
        count = 0
        for phrase in phrases:
            req = TTSRequest(
                text=phrase, lang=lang, voice_id=voice_id,
                tenant_id=tenant_id, session_id="warm",
            )
            result = await self.synthesize(req)
            if result.tier_used != "silence":
                count += 1
        return count

    def all_voices(self) -> list:
        voices = []
        for engine in self._engines.values():
            voices.extend(engine.voices())
        return voices


def _build_chain(active: str, available: list[str]) -> list[str]:
    rest = [n for n in available if n != active]
    return [active] + rest
