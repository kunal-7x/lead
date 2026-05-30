"""Prosody layer for natural, continuous Hindi telecaller speech.

Two production-grade classes used by the agent loop:

- :class:`SemanticChunkPlanner` — decides chunk (breath-group) boundaries on
  *thought units*, never mid-word. Streaming LLM tokens are fed in; the planner
  emits a chunk only when it reaches a clause/sentence boundary AND the minimum
  word budget, or when it hits a hard max-word ceiling. The first chunk is kept
  short for a fast first-audio onset. A too-short trailing clause is held and
  joined with the next chunk so we never emit a 2-word stub.

- :class:`ProsodyShaper` — SAFETY NET only (connector injection removed; the LLM
  now produces natural prosody via SPEECH OUTPUT RULES). Strips any stray
  markdown/symbols the LLM might accidentally emit (**, #, *, |, etc.) so they
  never reach TTS, and normalises whitespace/line-breaks. Does NOT inject
  connectors (देखिए/अच्छा/तो) — those come from the LLM naturally.

Both classes are pure / deterministic and fully unit-testable.
"""

from __future__ import annotations

import re

__all__ = ["SemanticChunkPlanner", "ProsodyShaper", "next_connector"]

# ── Boundary detection ────────────────────────────────────────────────────────
# Clause-final punctuation: Latin + Devanagari danda + ellipsis. A boundary here
# is a strong place to break a breath group.
_CLAUSE_FINAL = re.compile(r"[.?!।…]")
# Soft/comma boundaries: weaker, used for the fast first chunk and as a fallback
# break point inside a long run with no sentence end.
_SOFT_BOUNDARY = re.compile(r"[,،;:—–]")
# Hindi clause connectors — a natural place to start a new breath group when they
# appear at a word boundary. Matched as standalone words (space/za-start bounded)
# so we never break inside a longer word that merely contains these letters.
_CONNECTORS = ("तो", "लेकिन", "मतलब", "और", "क्योंकि", "इसलिए", "पर", "फिर")
_CONNECTOR_RE = re.compile(
    r"(?:^|\s)(?:" + "|".join(re.escape(c) for c in _CONNECTORS) + r")\s"
)


def _word_count(text: str) -> int:
    return len(text.split())


def _ends_with_boundary(text: str) -> bool:
    """True if the trimmed text ends on clause-final punctuation."""
    t = text.rstrip()
    return bool(t) and bool(_CLAUSE_FINAL.search(t[-1]))


class SemanticChunkPlanner:
    """Plan breath-group chunk boundaries on thought units.

    Fed incrementally with LLM token text via :meth:`feed`, which returns a list
    of completed chunks (often empty). At end of stream call :meth:`flush` to get
    whatever remains. Boundaries are chosen so that:

    * a chunk NEVER ends mid-word — splits only happen at whitespace following
      clause/soft punctuation or before a clause connector;
    * the FIRST chunk of a turn flushes early (>= ``first_words``) on any clause
      or soft boundary, for fast first-audio onset;
    * subsequent chunks need >= ``min_words`` and a clause-final boundary (or a
      connector start past the minimum) before flushing;
    * a hard ``max_words`` ceiling force-flushes at the latest safe word boundary
      so a runaway sentence still yields continuous audio;
    * a too-short trailing clause is held in the buffer and joined with the next
      content rather than emitted as a stub.

    Defaults mirror the breath-rhythm tuning that shipped in ``agent.py``:
    first ~5 words, min ~12, max ~35.
    """

    def __init__(
        self,
        min_words: int = 12,
        max_words: int = 35,
        first_words: int = 5,
    ) -> None:
        if first_words < 1:
            raise ValueError("first_words must be >= 1")
        if min_words < first_words:
            raise ValueError("min_words must be >= first_words")
        if max_words < min_words:
            raise ValueError("max_words must be >= min_words")
        self.min_words = min_words
        self.max_words = max_words
        self.first_words = first_words
        self._buf = ""
        self._first_chunk_pending = True

    def reset(self) -> None:
        """Reset state for a new turn (fresh first-chunk + empty buffer)."""
        self._buf = ""
        self._first_chunk_pending = True

    # ── streaming API ─────────────────────────────────────────────────────────
    def feed(self, token: str) -> list[str]:
        """Append ``token`` to the buffer and return any completed chunks.

        Returns a list (usually 0 or 1 items, possibly more if a long token
        crosses several max-word ceilings).
        """
        if token:
            self._buf += token
        out: list[str] = []
        while True:
            chunk = self._try_emit()
            if chunk is None:
                break
            out.append(chunk)
        return out

    def flush(self) -> str:
        """Return the remaining buffer (end-of-stream); clears the buffer.

        Always returns the trailing text even if below ``min_words`` — at end of
        stream there is nothing left to join it with.
        """
        rest = self._buf.strip()
        self._buf = ""
        self._first_chunk_pending = False
        return rest

    # ── boundary logic ──────────────────────────────────────────────────────--
    def _split_at(self, cut: int) -> str:
        """Emit buffer[:cut] as a chunk, keep the remainder. cut is a char index."""
        chunk = self._buf[:cut].strip()
        self._buf = self._buf[cut:].lstrip()
        if chunk:
            self._first_chunk_pending = False
        return chunk

    def _try_emit(self) -> str | None:
        """Attempt to emit one chunk from the current buffer; None if not ready."""
        buf = self._buf
        if not buf.strip():
            return None
        wc = _word_count(buf)

        # Hard ceiling: force-flush at the latest safe (whitespace) boundary so we
        # never block on a runaway sentence — but still never split mid-word.
        if wc >= self.max_words:
            cut = self._max_word_cut()
            if cut is not None:
                return self._split_at(cut)

        if self._first_chunk_pending:
            # Fast onset: flush on the first clause OR soft boundary where the
            # text up to that boundary already has at least first_words words.
            if wc >= self.first_words:
                cut = self._boundary_cut(include_soft=True, min_words=self.first_words)
                if cut is not None:
                    return self._split_at(cut)
            # If the first chunk is dragging on with no qualifying soft boundary,
            # fall through to the normal min_words + clause/connector logic below
            # so it still breaks naturally instead of waiting for the whole reply.

        # Subsequent chunks (and an over-long first chunk): need min_words AND a
        # clause-final boundary, or a connector start that begins a new breath
        # group past the minimum.
        if wc >= self.min_words:
            cut = self._boundary_cut(include_soft=False, min_words=self.min_words)
            if cut is not None:
                return self._split_at(cut)
            cut = self._connector_cut()
            if cut is not None:
                return self._split_at(cut)
        return None

    def _boundary_cut(self, include_soft: bool, min_words: int) -> int | None:
        """Char index just after the FIRST clause-final (and optionally soft)
        boundary that (a) is followed by whitespace or end-of-buffer (so the split
        lands on a word edge) and (b) has at least ``min_words`` words before it —
        avoiding tiny stub chunks created by an early comma. None if none qualify.
        """
        buf = self._buf
        for m in _CLAUSE_FINAL.finditer(buf):
            end = m.end()
            if end < len(buf) and not buf[end].isspace():
                continue
            if _word_count(buf[:end]) >= min_words:
                return end
        if include_soft:
            for m in _SOFT_BOUNDARY.finditer(buf):
                end = m.end()
                if end < len(buf) and not buf[end].isspace():
                    continue
                if _word_count(buf[:end]) >= min_words:
                    return end
        return None

    def _connector_cut(self) -> int | None:
        """Char index of a clause connector that starts a NEW breath group.

        We break *before* the connector (so 'तो' starts the next chunk) only if
        enough words precede it to satisfy min_words — joining a short leading
        clause forward instead of emitting a stub.
        """
        buf = self._buf
        for m in _CONNECTOR_RE.finditer(buf):
            # Start of the connector word (skip a leading space if matched).
            start = m.start()
            while start < len(buf) and buf[start].isspace():
                start += 1
            if start == 0:
                continue  # connector at the very start — nothing precedes it
            if _word_count(buf[:start]) >= self.min_words:
                return start
        return None

    def _max_word_cut(self) -> int | None:
        """Latest whitespace boundary at/under max_words; falls back to any
        boundary or the last whitespace so we never split mid-word."""
        buf = self._buf
        # Prefer a real punctuation boundary if one exists (any size — we're at
        # the ceiling, so the priority is to flush at the latest safe word edge).
        cut = self._boundary_cut(include_soft=True, min_words=1)
        if cut is not None:
            return cut
        # Otherwise cut at the whitespace after max_words words.
        words = buf.split()
        if len(words) <= self.max_words:
            return None
        # Find char offset of the (max_words)-th word's end.
        count = 0
        idx = 0
        for tok in re.finditer(r"\S+", buf):
            count += 1
            idx = tok.end()
            if count >= self.max_words:
                break
        return idx if idx > 0 else None


# ── Prosody shaping ───────────────────────────────────────────────────────────
# Normalisation patterns.
_MULTISPACE = re.compile(r"[ \t]{2,}")
_ELLIPSIS = re.compile(r"\.{3,}|…")
_NEWLINES = re.compile(r"\s*[\r\n]+\s*")
_DEVANAGARI = re.compile(r"[ऀ-ॿ]")
# Stray markdown/symbol patterns that should never reach TTS.
# Strips: **bold**, *italic*, __underline__, # headings, | pipes, → arrows,
# leading bullets (- or * at line start), {} [] angle brackets used as labels.
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
    2. Maps line-breaks → comma micro-pause.
    3. Normalises whitespace.

    ``reset_turn()`` kept for backward-compat (no-op now). ``allow_inject``
    parameter kept for backward-compat but has no effect.
    """

    # Kept for backward-compat with callers that reference CONNECTORS / next_connector.
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

    # ── helpers ─────────────────────────────────────────────────────────────--
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


def next_connector(turn_index: int, last_connector: str | None,
                   *, every_n: int = 3) -> str:
    """Kept for backward-compat. Connector injection is now handled by the LLM.

    Always returns "" (no injection). The LLM's SPEECH OUTPUT RULES produce
    natural connectors natively; the ProsodyShaper no longer injects them.
    """
    return ""
