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


def test_client_stream_without_done_raises(monkeypatch):
    """HttpTTSClient.synthesize_stream: if the router WS ends WITHOUT a 'done'
    frame (truncation / mid-relay drop), it must RAISE so _synth_and_play_stream
    falls back to batch REST — never silently treat the partial as complete."""
    import json as _json

    from voice_agent.clients import HttpTTSClient

    class _NoDoneWS:
        """Yields a binary chunk then ends the iterator with NO 'done' frame."""
        def __init__(self):
            self.close_code = None
            self.state = "OPEN"

        async def send(self, _data):
            pass

        async def close(self):
            self.close_code = 1000

        def __aiter__(self):
            async def _gen():
                yield b"\x11\x11" * 80   # one partial PCM chunk
                # iterator ends here: NO {"type":"done"} — simulates truncation
            return _gen()

    client = HttpTTSClient(base_url="http://x")

    async def _fake_ensure():
        client._stream_ws = _NoDoneWS()
        return client._stream_ws
    client._ensure_stream_ws = _fake_ensure  # type: ignore[assignment]

    async def _run():
        got = []
        raised = False
        try:
            async for c in client.synthesize_stream("namaste duniya", "hi-en", "priya"):
                got.append(c)
        except RuntimeError as exc:
            raised = "truncated" in str(exc)
        return got, raised

    got, raised = asyncio.run(_run())
    assert got == [b"\x11\x11" * 80]   # the partial chunk WAS yielded
    assert raised is True              # but absence of 'done' raised => fallback


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


# ── T1.1 synth-ahead pipeline (continuous playback, no inter-chunk gap) ───────


class _TimingTTS:
    """TTS whose synthesize() takes `synth_s` and records when each synth begins,
    so a test can assert that chunk N+1's synth overlaps chunk N's playback."""

    def __init__(self, synth_s: float = 0.05) -> None:
        self._synth_s = synth_s
        self.synth_starts: list[float] = []
        self.call_count = 0

    async def synthesize(self, text, lang, voice_id, tenant_id, session_id,
                         tts_premium=False):
        from voice_agent.clients import TTSResult
        self.call_count += 1
        self.synth_starts.append(asyncio.get_event_loop().time())
        await asyncio.sleep(self._synth_s)
        return TTSResult(audio=text.encode(), tier_used="t", cache_hit=False)


class _PipeCtx:
    lang = "hi-en"
    voice_profile_id = "priya"
    session_id = "s1"
    tenant_id = "t1"
    tts_premium = False


def _pipe_loop(tts) -> AgentLoop:
    loop = AgentLoop.__new__(AgentLoop)
    loop._tts = tts
    loop._stop_playback = asyncio.Event()
    loop._tts_chunk_seq = 0
    loop.ctx = _PipeCtx()
    loop._turn_index = 0
    return loop


def test_pipeline_overlaps_synth_with_playback(monkeypatch):
    """Synth of the NEXT chunk must start BEFORE the current chunk finishes
    playing → the audio buffer never empties between chunks (no robotic gap)."""
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", False)  # batch synth path
    tts = _TimingTTS(synth_s=0.05)
    loop = _pipe_loop(tts)

    play_log: list[tuple[str, float]] = []

    async def send_audio(frame: bytes):
        # Simulate real playback time for the frame.
        play_log.append(("start", asyncio.get_event_loop().time()))
        await asyncio.sleep(0.05)
        play_log.append(("end", asyncio.get_event_loop().time()))

    async def _run():
        q: asyncio.Queue = asyncio.Queue()
        await q.put(("chunk one", False))
        await q.put(("chunk two", False))
        await q.put(("chunk three", False))
        await q.put(("", True))
        await loop._run_synth_ahead_pipeline(q, send_audio, None)

    asyncio.run(_run())

    assert tts.call_count == 3
    # Chunk 2 synth must START before chunk 1 finishes PLAYING (overlap).
    first_play_end = next(t for k, t in play_log if k == "end")
    second_synth_start = tts.synth_starts[1]
    assert second_synth_start < first_play_end, (
        "next-chunk synth did not overlap current playback — queue would gap"
    )


def test_pipeline_plays_all_chunks_in_order(monkeypatch):
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", False)
    tts = _TimingTTS(synth_s=0.0)
    loop = _pipe_loop(tts)
    sent: list[bytes] = []

    async def send_audio(frame: bytes):
        sent.append(frame)

    async def _run():
        q: asyncio.Queue = asyncio.Queue()
        for s in ("a", "b", "c"):
            await q.put((s, False))
        await q.put(("", True))
        await loop._run_synth_ahead_pipeline(q, send_audio, None)

    asyncio.run(_run())
    assert sent == [b"a", b"b", b"c"]


def test_pipeline_first_audio_callback_fires_once(monkeypatch):
    monkeypatch.setattr(agent_mod, "_TTS_STREAMING_WS", False)
    tts = _TimingTTS(synth_s=0.0)
    loop = _pipe_loop(tts)
    fired = []

    def on_first():
        fired.append(1)

    async def send_audio(frame: bytes):
        pass

    async def _run():
        q: asyncio.Queue = asyncio.Queue()
        for s in ("a", "b"):
            await q.put((s, False))
        await q.put(("", True))
        await loop._run_synth_ahead_pipeline(q, send_audio, on_first)

    asyncio.run(_run())
    assert sum(fired) == 1  # exactly once, on the first chunk
