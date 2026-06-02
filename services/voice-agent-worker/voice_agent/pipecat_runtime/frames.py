"""Custom (non-pipecat) frames used inside this runtime.

Only ``BrainOutputFrame`` lives here: it carries a parsed ``BrainOutput`` from
the LlmRouterProcessor to the ActionsProcessor. We subclass pipecat's ``Frame``
when available (so it flows through a real pipeline) and fall back to the shim
``Frame`` otherwise.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

# API-CHECK: pipecat.frames.frames.Frame base class (for custom-frame subclassing).
try:  # pragma: no cover - exercised only with pipecat installed
    from pipecat.frames.frames import Frame  # type: ignore
except Exception:  # noqa: BLE001
    from voice_agent.pipecat_runtime._pipecat_shim import Frame


@dataclass
class BrainOutputFrame(Frame):
    """Carries the BrainOutput metadata from llm-router to ActionsProcessor."""

    brain: Any = None
