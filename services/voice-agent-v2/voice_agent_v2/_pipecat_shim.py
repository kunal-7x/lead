"""Minimal pipecat shims for voice-agent-v2 unit tests.

WHY THIS EXISTS: pipecat-ai pulls torch/onnxruntime and may not be installed
in the CI / test virtualenv. The LlmRouterProcessor tries real pipecat imports
first and falls back to these stubs so tests run on any machine without GPU
deps.

The shims mirror the pipecat 1.3 API surface that LlmRouterProcessor depends on:
  - FrameProcessor with process_frame / push_frame / cleanup
  - FrameDirection (DOWNSTREAM / UPSTREAM)
  - Frame dataclasses (TextFrame, TranscriptionFrame, LLM*Frame, etc.)

Ported from services/voice-agent-worker/voice_agent/pipecat_runtime/_pipecat_shim.py.
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
    """Base frame."""

    def __post_init__(self) -> None:  # pragma: no cover
        pass


@dataclass
class TextFrame(Frame):
    text: str = ""


@dataclass
class TranscriptionFrame(Frame):
    text: str = ""
    user_id: str = ""
    timestamp: str = ""
    confidence: float | None = None
    finalized: bool = True  # True = final STT commit; False = interim/partial


@dataclass
class InterimTranscriptionFrame(Frame):
    text: str = ""
    user_id: str = ""
    timestamp: str = ""
    finalized: bool = False


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
    """Minimal stand-in that records pushed frames for unit-test assertions."""

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        self.pushed_frames: list[tuple[Frame, FrameDirection]] = []
        self._next: "FrameProcessor | None" = None

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        return None

    async def push_frame(
        self,
        frame: Frame,
        direction: FrameDirection = FrameDirection.DOWNSTREAM,
    ) -> None:
        self.pushed_frames.append((frame, direction))
        if self._next is not None:
            await self._next.process_frame(frame, direction)

    def link(self, nxt: "FrameProcessor") -> None:
        self._next = nxt

    async def cleanup(self) -> None:
        return None


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
]
