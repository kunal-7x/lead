from __future__ import annotations

from abc import ABC, abstractmethod

SILENCE_THRESHOLD_MS = 750   # silence after this → end of utterance.
# Raised 500→750ms (T1.3): at 500ms the AI grabbed the turn on a mid-sentence
# breath/pause. 750ms tolerates a natural pause while still ending the turn
# promptly. Tunable via SILENCE_THRESHOLD_MS env (read by the caller, not here).
CHUNK_MS = 20                # 20ms per audio chunk at 8kHz
BYTES_PER_CHUNK = 8000 * CHUNK_MS // 1000 * 2  # 320 bytes


class VAD(ABC):
    """Voice Activity Detector interface."""

    @abstractmethod
    def is_speech(self, pcm_chunk: bytes) -> bool:
        """Return True if chunk contains speech."""
        ...

    @abstractmethod
    def reset(self) -> None:
        """Reset internal state between utterances."""
        ...

    def energy(self, pcm_chunk: bytes) -> float:
        """RMS energy of a PCM16 8kHz chunk (0.0 if too short).

        Used by the barge-in path to reject low-energy acoustic echo: real
        caller speech is clearly louder than the residual echo of the bot's
        own TTS bleeding back into the inbound track. Default impl works for
        any concrete VAD; overridden only if a subclass needs custom behaviour.
        """
        import struct
        if len(pcm_chunk) < 2:
            return 0.0
        samples = struct.unpack(f"<{len(pcm_chunk)//2}h", pcm_chunk)
        return (sum(s * s for s in samples) / len(samples)) ** 0.5


class FakeVAD(VAD):
    """Test VAD: speech for first N chunks, then silence.

    Configurable speech/silence pattern for deterministic tests.
    """

    def __init__(self, speech_chunks: int = 10) -> None:
        self._speech_chunks = speech_chunks
        self._count = 0

    def is_speech(self, pcm_chunk: bytes) -> bool:
        self._count += 1
        return self._count <= self._speech_chunks

    def reset(self) -> None:
        self._count = 0


class EnergyVAD(VAD):
    """Simple energy-threshold VAD — no torch dependency.

    Usable on CPU without Silero. Threshold tuned for 8kHz L16 PCM.
    For production use SileroVAD (below) for better accuracy.
    """

    def __init__(self, threshold: float = 500.0) -> None:
        self._threshold = threshold

    def is_speech(self, pcm_chunk: bytes) -> bool:
        import struct
        if len(pcm_chunk) < 2:
            return False
        samples = struct.unpack(f"<{len(pcm_chunk)//2}h", pcm_chunk)
        rms = (sum(s * s for s in samples) / len(samples)) ** 0.5
        return rms > self._threshold

    def reset(self) -> None:
        pass


class SileroVAD(VAD):
    """Silero VAD — 1.8MB model, MIT license, CPU-only, <5ms per chunk.

    Production: requires torch. Falls back to EnergyVAD if torch not installed.
    torch.hub downloads the model on first use (~1.8MB).
    """

    def __init__(self) -> None:
        self._model = None
        self._utils = None
        self._fallback = EnergyVAD()
        self._loaded = False

    def _load(self) -> bool:
        if self._loaded:
            return self._model is not None
        try:
            import torch
            model, utils = torch.hub.load(
                "snakers4/silero-vad", "silero_vad",
                force_reload=False, trust_repo=True, verbose=False,
            )
            self._model = model
            self._utils = utils
            self._loaded = True
            return True
        except Exception:
            self._loaded = True
            return False

    def is_speech(self, pcm_chunk: bytes) -> bool:
        if not self._load() or self._model is None:
            return self._fallback.is_speech(pcm_chunk)
        try:
            import torch
            import struct
            samples = struct.unpack(f"<{len(pcm_chunk)//2}h", pcm_chunk)
            tensor = torch.tensor(samples, dtype=torch.float32) / 32768.0
            prob = self._model(tensor.unsqueeze(0), 8000).item()
            return prob > 0.5
        except Exception:
            return self._fallback.is_speech(pcm_chunk)

    def reset(self) -> None:
        self._fallback.reset()


def collect_utterance(chunks: list[bytes], vad: VAD, silence_ms: int = SILENCE_THRESHOLD_MS) -> bytes:
    """Given audio chunks and VAD, return buffered speech bytes.

    Used in tests to simulate utterance collection synchronously.
    """
    silence_chunks_needed = silence_ms // CHUNK_MS
    speech_started = False
    silence_count = 0
    buffer = bytearray()

    for chunk in chunks:
        speech = vad.is_speech(chunk)
        if speech:
            speech_started = True
            silence_count = 0
            buffer.extend(chunk)
        elif speech_started:
            silence_count += 1
            buffer.extend(chunk)
            if silence_count >= silence_chunks_needed:
                break

    return bytes(buffer)
