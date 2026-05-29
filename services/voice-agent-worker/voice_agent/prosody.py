"""Prosody layer for natural, continuous Hindi telecaller speech.

Two production-grade classes used by the agent loop to make Sarvam Bulbul speech
sound natural instead of robotic:

- :class:`SemanticChunkPlanner` — decides chunk (breath-group) boundaries on
  *thought units*, never mid-word. Streaming LLM tokens are fed in; the planner
  emits a chunk only when it reaches a clause/sentence boundary AND the minimum
  word budget, or when it hits a hard max-word ceiling. The first chunk is kept
  short for a fast first-audio onset. A too-short trailing clause is held and
  joined with the next chunk so we never emit a 2-word stub.

- :class:`ProsodyShaper` — shapes the chunk *text* (NO SSML — bulbul ignores it)
  for natural delivery: keeps Devanagari intact, normalises pause punctuation to
  natural micro-pauses, and OPTIONALLY injects a single sparse, natural Hindi
  connector (देखिए / मतलब / तो) at a clause start where it reads naturally — at
  most once per turn, driven by simple state (never random, never a bare
  standalone filler).

Both classes are pure / deterministic (the shaper's injection is gated by a
per-turn counter, not randomness) so they are fully unit-testable.
"""

from __future__ import annotations

import re

__all__ = ["SemanticChunkPlanner", "ProsodyShaper"]

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
# Map repeated/odd pause punctuation to a single natural micro-pause. Sarvam
# bulbul honours a comma as a short pause and a danda/period as a longer one.
_MULTISPACE = re.compile(r"[ \t]{2,}")
_ELLIPSIS = re.compile(r"\.{3,}|…")
_NEWLINES = re.compile(r"\s*[\r\n]+\s*")
# A "bare filler" = the connector standing entirely alone as the whole chunk.
# We must never produce one of these.
_DEVANAGARI = re.compile(r"[ऀ-ॿ]")


class ProsodyShaper:
    """Shape chunk text for natural Sarvam speech (no SSML).

    * Keeps Hindi in Devanagari untouched (only normalises whitespace/pauses).
    * Maps line-breaks → comma micro-pause, collapses multi-space, and converts
      an ellipsis ``…`` / ``...`` to a Devanagari comma so bulbul renders a soft
      trailing pause instead of reading dots.
    * OPTIONALLY injects ONE sparse natural connector (देखिए / मतलब / तो) at a
      clause start, at most once per turn (gated by an internal counter, NOT
      random), and only when the chunk does not already begin with a connector
      and is long enough to carry it naturally. Never injects a bare standalone
      filler.

    Call :meth:`reset_turn` at the start of each agent turn so the once-per-turn
    injection budget is restored.
    """

    # Conservative, natural openers. देखिए = "look/see", मतलब = "meaning/so",
    # तो = "so". These are common, polite telecaller discourse markers.
    _INJECT_CONNECTOR = "देखिए"
    # Don't inject if the chunk already starts with any of these (avoid doubling).
    _LEADING_MARKERS = ("देखिए", "मतलब", "तो", "अच्छा", "हाँ", "जी", "अरे")
    # Only inject on a reasonably substantial chunk so the marker reads as part of
    # a real clause, never as a standalone filler.
    _MIN_WORDS_FOR_INJECT = 4

    def __init__(self, max_injections_per_turn: int = 1) -> None:
        self.max_injections_per_turn = max_injections_per_turn
        self._injections_used = 0
        self._chunk_index = 0

    def reset_turn(self) -> None:
        """Reset the per-turn injection budget and chunk counter."""
        self._injections_used = 0
        self._chunk_index = 0

    def shape(self, text: str, *, allow_inject: bool = True) -> str:
        """Return the prosody-shaped text for one chunk.

        ``allow_inject`` lets the caller suppress connector injection on chunks
        where it would be inappropriate (e.g. a question the user must answer).
        """
        shaped = self._normalize(text)
        if not shaped:
            return shaped
        idx = self._chunk_index
        self._chunk_index += 1
        if (
            allow_inject
            and self._injections_used < self.max_injections_per_turn
            and self._should_inject(shaped, idx)
        ):
            shaped = self._inject(shaped)
            self._injections_used += 1
        return shaped

    # ── helpers ─────────────────────────────────────────────────────────────--
    def _normalize(self, text: str) -> str:
        s = text.strip()
        if not s:
            return ""
        # Line breaks become a soft comma pause, not a hard cut.
        s = _NEWLINES.sub(", ", s)
        # Ellipsis → Devanagari comma (soft trailing pause); bulbul reads "…" oddly.
        s = _ELLIPSIS.sub("، ", s)
        # Collapse runs of spaces/tabs.
        s = _MULTISPACE.sub(" ", s)
        # Tidy space-before-punctuation introduced by normalisation.
        s = re.sub(r"\s+([،,.?!।])", r"\1", s)
        return s.strip()

    def _has_devanagari(self, text: str) -> bool:
        return bool(_DEVANAGARI.search(text))

    def _should_inject(self, shaped: str, chunk_index: int) -> bool:
        # Inject only into Hindi chunks, not on the very first chunk (keep onset
        # snappy), only on chunks long enough to carry the marker, and never when
        # the chunk already opens with a discourse marker.
        if chunk_index == 0:
            return False
        if not self._has_devanagari(shaped):
            return False
        if _word_count(shaped) < self._MIN_WORDS_FOR_INJECT:
            return False
        first_word = shaped.split(maxsplit=1)[0].strip("।,.?!، ")
        if first_word in self._LEADING_MARKERS:
            return False
        return True

    def _inject(self, shaped: str) -> str:
        # Prepend the connector + comma micro-pause. Result is never a bare filler
        # because shaped is a substantial clause (guarded by _should_inject).
        return f"{self._INJECT_CONNECTOR}، {shaped}"
