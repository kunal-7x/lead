#!/usr/bin/env python3
"""LIVE end-to-end test of the REAL SarvamStreamingSession against Sarvam WS.

Exercises the actual engine code (idle-based completion, µ-law->PCM16 decode,
v2 speaker mapping) with a ~30-word Hindi sentence and a short follow-up, on the
same persistent WS, and reports first_chunk_ms + total audio seconds + that the
stream completed without raising _StreamTruncated. Run on the droplet with
SARVAM_API_KEY in env.
"""
import asyncio
import os
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "services", "tts-router"))
from tts_router.engines.sarvam import (  # noqa: E402
    SarvamBulbulEngine, SarvamStreamingSession, _StreamTruncated,
)

LONG = (
    "नमस्ते, मैं आपकी कंपनी की तरफ से बात कर रही हूँ और मैं आपको हमारी नई सेवाओं "
    "के बारे में पूरी जानकारी देना चाहती हूँ ताकि आप सही निर्णय ले सकें और हमसे "
    "जुड़ सकें धन्यवाद।"
)
SHORT = "जी हाँ, बिल्कुल सही कहा आपने।"


async def one(session, text, label):
    t0 = time.time()
    n = 0
    samples = 0  # PCM16 samples (bytes/2)
    fms = -1
    truncated = False
    try:
        async for pcm in session.synthesize(text, "priya", "hi-IN"):
            if fms < 0:
                fms = int((time.time() - t0) * 1000)
            n += 1
            samples += len(pcm) // 2
    except _StreamTruncated as exc:
        truncated = True
        print("%s TRUNCATED %r" % (label, exc))
    dur = samples / 8000.0
    print("%s chunks=%d first_chunk_ms=%d pcm16_samples=%d audio_seconds=%.2f truncated=%s"
          % (label, n, fms, samples, dur, truncated))


async def main():
    if not os.getenv("SARVAM_API_KEY", "").strip():
        print("no_key"); return
    engine = SarvamBulbulEngine()
    async with SarvamStreamingSession(engine) as session:
        await one(session, LONG, "LONG_30w")
        await one(session, SHORT, "SHORT_followup")


if __name__ == "__main__":
    asyncio.run(main())
