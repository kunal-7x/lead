"""Tests for flag-gated Sarvam streaming-TTS path in the voice-agent-worker.

Verifies:
  - TTS_STREAMING_WS=false (default) → _synth_and_play_stream is a no-op (batch used).
  - TTS_STREAMING_WS=true  → streaming chunks are fed to send_audio as they arrive.
  - Streaming error → falls back (returns False) so the caller uses batch synth.
  - Barge-in mid-stream stops consuming/sending immediately.
All offline — FakeTTS provides synthesize_stream. The module-level flag is patched
directly (no module reload) to avoid disturbing other test files.
"""
from __future__ import annotations

import asyncio

import voice_agent.agent as agent_mod
from voice_agent.agent import AgentLoop
from tests.fakes.fake_services import FakeTTS

_PCM = b"\x11\x11" * 80  # 160 bytes PCM16 chunk


class _Ctx:
    lang = "hi-en"
    voice_profile_id = "priya"
    session_id = "s1"


def _bare_loop(tts) -> AgentLoop:
    loop = AgentLoop.__new__(AgentLoop)  # skip __init__
    loop._tts = tts
    loop._stop_playback = asyncio.Event()
    loop.ctx = _Ctx()
    return loop


def test_flag_off_no_streaming(monkeypatch):
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", False)
    tts = FakeTTS(stream_chunks=[_PCM, _PCM])
    loop = _bare_loop(tts)
    sent = []

    async def send_audio(a):
        sent.append(a)

    async def _run():
        return await loop._synth_and_play_stream("namaste", send_audio)

    result = asyncio.run(_run())
    assert result is False            # disabled → caller uses batch synth
    assert tts.stream_call_count == 0
    assert sent == []


def test_flag_on_streams_chunks(monkeypatch):
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", True)
    tts = FakeTTS(stream_chunks=[_PCM, _PCM, _PCM])
    loop = _bare_loop(tts)
    sent = []

    async def send_audio(a):
        sent.append(a)

    async def _run():
        return await loop._synth_and_play_stream("namaste", send_audio)

    result = asyncio.run(_run())
    assert result is True
    assert tts.stream_call_count == 1
    assert sent == [_PCM, _PCM, _PCM]  # fed as they arrive


def test_flag_on_error_falls_back(monkeypatch):
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", True)
    tts = FakeTTS(stream_raises=True)
    loop = _bare_loop(tts)

    async def send_audio(a):
        pass

    async def _run():
        return await loop._synth_and_play_stream("namaste", send_audio)

    result = asyncio.run(_run())
    assert result is False  # error → caller uses batch synth


def test_bargein_stops_stream(monkeypatch):
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", True)
    tts = FakeTTS(stream_chunks=[_PCM] * 10)
    loop = _bare_loop(tts)
    sent = []

    async def send_audio(a):
        sent.append(a)
        loop._stop_playback.set()  # barge-in after first chunk

    async def _run():
        return await loop._synth_and_play_stream("namaste", send_audio)

    asyncio.run(_run())
    assert len(sent) == 1  # aborted before second chunk
