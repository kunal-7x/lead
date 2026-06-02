"""Prosody shaping — ProsodyShaper safety net only.

Ported from services/voice-agent-worker/voice_agent/prosody.py.
Only ProsodyShaper is ported (SemanticChunkPlanner is not needed in the
LiveKit pipeline since Pipecat handles sentence aggregation natively).

ProsodyShaper strips stray markdown/symbols before TTS and normalises
whitespace. It does NOT inject connectors — those come from the LLM naturally
via SPEECH OUTPUT RULES in the system prompt.
"""

from __future__ import annotations

import re

__all__ = ["ProsodyShaper"]

# Normalisation patterns.
_MULTISPACE = re.compile(r"[ \t]{2,}")
_ELLIPSIS = re.compile(r"\.{3,}|…")
_NEWLINES = re.compile(r"\s*[\r\n]+\s*")
_DEVANAGARI = re.compile(r"[ऀ-ॿ]")
# Stray markdown/symbol patterns that should never reach TTS.
_MARKDOWN_BOLD_ITALIC = re.compile(r"\*{1,3}([^*]*)\*{1,3}")
_MARKDOWN_UNDERLINE = re.compile(r"_{1,2}([^_]*)_{1,2}")
_MARKDOWN_HEADING = re.compile(r"(?m)^#{1,6}\s*")
_MARKDOWN_BULLET = re.compile(r"(?m)^[\-\*]\s+")
_MARKDOWN_PIPE = re.compile(r"\|")
_MARKDOWN_ARROW = re.compile(r"→|->|=>")
_MARKDOWN_BRACKETS = re.compile(r"[\[\]{}<>]")
_MARKDOWN_BACKTICK = re.compile(r"`+")


class ProsodyShaper:
    """SAFETY NET: strip stray markdown/symbols before TTS; normalise whitespace.

    Connector injection (देखिए/अच्छा/तो) has been REMOVED — prosody now comes
    from the LLM natively via SPEECH OUTPUT RULES in the system prompt.

    ``shape()`` only:
    1. Strips stray markdown/symbols (**, *, #, |, →, [], {}, backticks).
    2. Maps line-breaks to comma micro-pause.
    3. Normalises whitespace.

    ``reset_turn()`` kept for backward-compat (no-op now). ``allow_inject``
    parameter kept for backward-compat but has no effect.
    """

    # Kept for backward-compat with callers that reference CONNECTORS.
    CONNECTORS = ("देखिए", "अच्छा", "तो", "मतलब", "हाँ तो")

    def __init__(self, max_injections_per_turn: int = 1,
                 connector: str | None = None) -> None:
        # All injection params are ignored; kept for drop-in backward-compat.
        self._chunk_index = 0

    def reset_turn(self) -> None:
        """Reset chunk counter (no-op for injection; kept for compat)."""
        self._chunk_index = 0

    def shape(self, text: str, *, allow_inject: bool = True) -> str:  # noqa: ARG002
        """Strip markdown/symbols and normalise whitespace. No connector injection."""
        shaped = self._normalize(text)
        self._chunk_index += 1
        return shaped

    # ── helpers ─────────────────────────────────────────────────────────────────
    def _normalize(self, text: str) -> str:
        s = text.strip()
        if not s:
            return ""
        # Strip markdown bold/italic/underline (keep inner text).
        s = _MARKDOWN_BOLD_ITALIC.sub(r"\1", s)
        s = _MARKDOWN_UNDERLINE.sub(r"\1", s)
        # Strip heading markers.
        s = _MARKDOWN_HEADING.sub("", s)
        # Strip leading bullet markers.
        s = _MARKDOWN_BULLET.sub("", s)
        # Strip pipes (table separators), arrows, brackets, backticks.
        s = _MARKDOWN_PIPE.sub(" ", s)
        s = _MARKDOWN_ARROW.sub(" ", s)
        s = _MARKDOWN_BRACKETS.sub("", s)
        s = _MARKDOWN_BACKTICK.sub("", s)
        # Line breaks become a soft comma pause.
        s = _NEWLINES.sub(", ", s)
        # Ellipsis → short pause marker (retain as comma); bulbul reads "…" oddly.
        s = _ELLIPSIS.sub(", ", s)
        # Collapse runs of spaces/tabs.
        s = _MULTISPACE.sub(" ", s)
        # Tidy space-before-punctuation introduced by normalisation.
        s = re.sub(r"\s+([,,.?!।])", r"\1", s)
        return s.strip()

    def _has_devanagari(self, text: str) -> bool:
        return bool(_DEVANAGARI.search(text))
