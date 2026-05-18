from __future__ import annotations

import struct

from voice_agent.vad import FakeVAD, EnergyVAD, collect_utterance, CHUNK_MS


def _speech_chunk() -> bytes:
    return struct.pack("<160h", *([2000] * 160))


def _silence_chunk() -> bytes:
    return b"\x00\x00" * 160


def test_fake_vad_speech_then_silence():
    vad = FakeVAD(speech_chunks=5)
    results = [vad.is_speech(_silence_chunk()) for _ in range(10)]
    assert results[:5] == [True] * 5
    assert results[5:] == [False] * 5


def test_fake_vad_reset():
    vad = FakeVAD(speech_chunks=3)
    for _ in range(3):
        vad.is_speech(_silence_chunk())
    assert not vad.is_speech(_silence_chunk())
    vad.reset()
    assert vad.is_speech(_silence_chunk())  # reset to speech


def test_silence_triggers_stt():
    """VAD detects speech then silence → collect_utterance returns speech bytes."""
    vad = FakeVAD(speech_chunks=5)
    chunks = [_speech_chunk()] * 5 + [_silence_chunk()] * 40
    utterance = collect_utterance(chunks, vad, silence_ms=700)
    # Should have buffered some audio
    assert len(utterance) > 0


def test_energy_vad_speech():
    vad = EnergyVAD(threshold=500)
    assert vad.is_speech(_speech_chunk()) is True


def test_energy_vad_silence():
    vad = EnergyVAD(threshold=500)
    assert vad.is_speech(_silence_chunk()) is False


def test_energy_vad_reset_noop():
    vad = EnergyVAD()
    vad.reset()  # should not raise


def test_collect_utterance_empty():
    vad = FakeVAD(speech_chunks=0)
    chunks = [_silence_chunk()] * 50
    result = collect_utterance(chunks, vad)
    assert result == b""


def test_collect_utterance_no_silence():
    """If never goes silent, returns all buffered speech."""
    vad = FakeVAD(speech_chunks=1000)
    chunks = [_speech_chunk()] * 5
    result = collect_utterance(chunks, vad)
    assert len(result) == len(_speech_chunk()) * 5
