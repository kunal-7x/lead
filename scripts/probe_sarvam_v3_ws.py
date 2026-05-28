#!/usr/bin/env python3
"""Direct bulbul:v3 WS probe — verifies the mid-word-cutoff fix end-to-end.

Connects to wss://api.sarvam.ai/text-to-speech/ws with model IN the config frame
(NOT the URL), output_audio_codec=mulaw, speech_sample_rate=8000, speaker=priya.
Sends a ~30-word Hindi sentence, then flush, and reports:
  - connected (no 403)
  - first audio frame shape (so we can confirm field names + that it's µ-law 8k)
  - first_chunk_ms
  - whether a real completion (done/flush_done) event arrived BEFORE any close
  - total µ-law bytes received and the implied audio duration (bytes/8000 s)

Run on the droplet WITH env sourced so SARVAM_API_KEY is present:
  python3 /root/lead/scripts/probe_sarvam_v3_ws.py
"""
import asyncio
import base64
import json
import os
import time

import websockets

WS_URL = "wss://api.sarvam.ai/text-to-speech/ws"
KEY = os.getenv("SARVAM_API_KEY", "").strip()

# ~30 Hindi words (the case that truncated mid-word before the fix).
TEXT = (
    "नमस्ते, मैं आपकी कंपनी की तरफ से बात कर रही हूँ और मैं आपको हमारी नई सेवाओं "
    "के बारे में पूरी जानकारी देना चाहती हूँ ताकि आप सही निर्णय ले सकें और हमसे "
    "जुड़ सकें धन्यवाद।"
)

CONFIG = {
    "type": "config",
    "data": {
        "model": "bulbul:v3",
        "target_language_code": "hi-IN",
        "speaker": "priya",
        "pace": 1.0,
        "temperature": 0.6,
        "enable_preprocessing": True,
        "output_audio_codec": "mulaw",
        "speech_sample_rate": 8000,
        "min_buffer_size": 50,
        "max_chunk_length": 250,
    },
}
_COMPLETION = ("flush_done", "done", "complete", "completion")


async def main() -> int:
    if not KEY:
        print("RESULT no_key=1")
        return 1
    headers = {"API-Subscription-Key": KEY}
    t0 = time.time()
    first_ms = -1
    total_ulaw = 0
    n_audio = 0
    completed = False
    closed_before_complete = False
    first_shape = None
    try:
        async with websockets.connect(
            WS_URL, additional_headers=headers, open_timeout=5, close_timeout=3
        ) as ws:
            print("RESULT connected=1 (no 403)")
            await ws.send(json.dumps(CONFIG))
            await ws.send(json.dumps(
                {"type": "text", "data": {"text": TEXT, "send_completion_event": True}}))
            await ws.send(json.dumps({"type": "flush"}))
            try:
                async for raw in ws:
                    if isinstance(raw, bytes):
                        if first_shape is None:
                            first_shape = f"binary len={len(raw)}"
                        if first_ms < 0:
                            first_ms = int((time.time() - t0) * 1000)
                        total_ulaw += len(raw)
                        n_audio += 1
                        continue
                    msg = json.loads(raw)
                    mtype = msg.get("type", "")
                    if mtype in ("audio", "audio_chunk"):
                        d = msg.get("data", {})
                        b64 = d.get("audio") or d.get("audio_chunk") or ""
                        if first_shape is None:
                            first_shape = "json type=%s keys=%s" % (mtype, list(d.keys()))
                        if b64:
                            if first_ms < 0:
                                first_ms = int((time.time() - t0) * 1000)
                            total_ulaw += len(base64.b64decode(b64))
                            n_audio += 1
                    elif mtype in _COMPLETION:
                        completed = True
                        break
                    elif mtype == "error":
                        print("RESULT error_frame=%r" % msg)
                        return 2
            except websockets.exceptions.ConnectionClosed as exc:
                if not completed:
                    closed_before_complete = True
                    print("RESULT closed_before_completion=1 exc=%r" % exc)
    except Exception as exc:  # noqa: BLE001
        print("RESULT connect_or_proto_error=%r" % exc)
        return 3

    dur_s = total_ulaw / 8000.0  # µ-law 8k = 1 byte/sample
    print("RESULT first_shape=%s" % first_shape)
    print("RESULT first_chunk_ms=%d n_audio_frames=%d total_ulaw_bytes=%d audio_seconds=%.2f"
          % (first_ms, n_audio, total_ulaw, dur_s))
    print("RESULT completion_received=%s closed_before_complete=%s"
          % (completed, closed_before_complete))
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
