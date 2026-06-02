"""Minimal stand-ins for the Pipecat symbols this package references.

WHY THIS EXISTS: the real ``pipecat-ai`` stack pulls torch/onnxruntime and only
installs cleanly on the Linux droplet. To let the runtime IMPORT and to unit-test
the custom processors (LlmRouterProcessor, CsoProcessor, ActionsProcessor) with
fully mocked network/frames on any machine, each processor module tries the real
pipecat import first and falls back to these shims.

The shims mirror the DOCUMENTED pipecat 1.3 API surface we depend on:
  * ``FrameProcessor`` with ``process_frame`` / ``push_frame`` / ``cleanup``,
  * ``FrameDirection`` (DOWNSTREAM / UPSTREAM),
  * the frame dataclasses we emit/consume.

When the real library is present these shims are NOT used — every call site keeps
an ``# API-CHECK:`` note flagging the exact symbol to verify against the installed
version. The shims are intentionally behaviour-light: they exist for import +
isolated processor logic tests, NOT to emulate the Pipecat runtime.
"""

from __future__ import annotations

import enum
from dataclasses import dataclass, field
from typing import Any


class FrameDirection(enum.Enum):
    DOWNSTREAM = 1
    UPSTREAM = 2


@dataclass
class Frame:
    """Base frame. Real pipecat frames carry ids/metadata; we keep it minimal."""

    def __post_init__(self) -> None:  # pragma: no cover - trivial
        pass


@dataclass
class TextFrame(Frame):
    text: str = ""


@dataclass
class TranscriptionFrame(Frame):
    text: str = ""
    user_id: str = ""
    timestamp: str = ""
    # Not in stock pipecat TranscriptionFrame; some STT services attach it. We
    # read it defensively via getattr, so its presence here is harmless.
    confidence: float | None = None


@dataclass
class InterimTranscriptionFrame(Frame):
    text: str = ""
    user_id: str = ""
    timestamp: str = ""


@dataclass
class LLMFullResponseStartFrame(Frame):
    pass


@dataclass
class LLMFullResponseEndFrame(Frame):
    pass


@dataclass
class StartInterruptionFrame(Frame):
    pass


@dataclass
class EndFrame(Frame):
    pass


@dataclass
class TTSAudioRawFrame(Frame):
    audio: bytes = b""
    sample_rate: int = 8000
    num_channels: int = 1


class FrameProcessor:
    """Light stand-in for pipecat.processors.frame_processor.FrameProcessor.

    Records pushed frames into ``pushed_frames`` so unit tests can assert on the
    exact frame sequence a processor emits without a running pipeline.
    """

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        self.pushed_frames: list[tuple[Frame, FrameDirection]] = []
        self._next: "FrameProcessor | None" = None

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        # Real base records metrics/handles system frames; nothing to do here.
        return None

    async def push_frame(
        self, frame: Frame, direction: FrameDirection = FrameDirection.DOWNSTREAM
    ) -> None:
        self.pushed_frames.append((frame, direction))
        if self._next is not None:
            await self._next.process_frame(frame, direction)

    def link(self, nxt: "FrameProcessor") -> None:
        self._next = nxt

    async def cleanup(self) -> None:
        return None


# Custom (non-pipecat) frame used to hand a parsed BrainOutput to ActionsProcessor.
@dataclass
class BrainOutputFrame(Frame):
    brain: Any = None


__all__ = [
    "Frame",
    "FrameDirection",
    "FrameProcessor",
    "TextFrame",
    "TranscriptionFrame",
    "InterimTranscriptionFrame",
    "LLMFullResponseStartFrame",
    "LLMFullResponseEndFrame",
    "StartInterruptionFrame",
    "EndFrame",
    "TTSAudioRawFrame",
    "BrainOutputFrame",
]
