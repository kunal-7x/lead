"""Pipecat runtime — parallel, feature-flagged replacement for the hand-rolled
audio loop in voice-agent-worker.

This package is ADDITIVE. The old worker (voice_agent.app / voice_agent.agent)
stays runnable for rollback. Selection is via the ``VOICE_RUNTIME`` env flag
(``old`` | ``pipecat``); nothing here imports or mutates the old modules.

Architecture ("targeted hybrid"): Pipecat owns the Vobiz WebSocket transport,
serializer, VAD, turn detection, interruption handling, and the STT/TTS sockets
(direct provider calls — the dead-air fix). The brain stays in ``llm-router``,
called from a custom ``LlmRouterProcessor``. CSO/actions/prosody/models are
reused as libraries from the parent package.

See PIPECAT_RUNTIME_DESIGN.md for the full spec.
"""

from __future__ import annotations

__all__ = ["__version__"]

__version__ = "0.1.0"
