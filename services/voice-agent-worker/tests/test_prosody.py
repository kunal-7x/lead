"""Unit tests for the prosody layer (SemanticChunkPlanner + ProsodyShaper)."""

from __future__ import annotations

from voice_agent.prosody import ProsodyShaper, SemanticChunkPlanner, next_connector


def _feed_words(planner: SemanticChunkPlanner, text: str) -> list[str]:
    """Feed text word-by-word (simulating token streaming) and collect chunks."""
    out: list[str] = []
    words = text.split(" ")
    for i, w in enumerate(words):
        tok = w if i == 0 else " " + w
        out.extend(planner.feed(tok))
    rest = planner.flush()
    if rest:
        out.append(rest)
    return out


# ── SemanticChunkPlanner ──────────────────────────────────────────────────────


def test_never_emits_midword_chunk():
    planner = SemanticChunkPlanner(min_words=12, max_words=35, first_words=5)
    text = (
        "नमस्ते सर मैं आपकी कैसे मदद कर सकती हूँ बताइए आपको क्या जानकारी चाहिए "
        "और हम आपके लिए सबसे अच्छा प्लान भी निकाल सकते हैं बिल्कुल आसानी से।"
    )
    chunks = _feed_words(planner, text)
    # Every emitted chunk must be a prefix-reconstructable, whitespace-bounded run:
    # rejoining all chunks and the original (whitespace-normalised) must match.
    joined = " ".join(chunks).split()
    assert joined == text.split(), "chunks lost/split words"
    # No chunk may start or end inside a word: each chunk's tokens are full words.
    for c in chunks:
        assert c == c.strip()
        for tok in c.split():
            assert tok in text.split() or tok.rstrip("।,.?!") in [
                w.rstrip("।,.?!") for w in text.split()
            ]


def test_respects_max_words_ceiling():
    planner = SemanticChunkPlanner(min_words=12, max_words=20, first_words=5)
    # A long run with no punctuation must still be force-flushed under the ceiling.
    text = " ".join(f"शब्द{i}" for i in range(40))
    chunks = _feed_words(planner, text)
    assert len(chunks) >= 2
    for c in chunks[:-1]:  # non-final chunks bounded by ceiling
        assert len(c.split()) <= planner.max_words


def test_respects_min_words_holds_short_clause():
    planner = SemanticChunkPlanner(min_words=12, max_words=35, first_words=5)
    # First chunk is allowed to be small; a short SECOND clause must be held and
    # joined, not emitted as a stub.
    chunks = _feed_words(
        planner,
        "अच्छा ठीक है। हाँ। मैं आपको पूरी जानकारी विस्तार से देता हूँ अभी इसी समय।",
    )
    # The tiny "हाँ।" clause must not appear as its own 1-word chunk.
    assert "हाँ।" not in chunks
    # Aside from the first (fast-onset) chunk, every chunk meets the minimum,
    # except the final flushed remainder.
    body = chunks[1:-1] if len(chunks) > 2 else []
    for c in body:
        assert len(c.split()) >= planner.min_words


def test_first_chunk_small_for_fast_onset():
    planner = SemanticChunkPlanner(min_words=12, max_words=35, first_words=5)
    chunks = _feed_words(
        planner,
        "नमस्ते जी मैं प्रिया बोल रही हूँ, आपकी कैसे सहायता कर सकती हूँ आज बताइए जरूर।",
    )
    assert chunks, "expected at least one chunk"
    # First chunk should flush early at the comma (>= first_words, < min_words):
    # the comma falls after 6 words here, so onset is fast without a stub.
    assert len(chunks[0].split()) < planner.min_words
    assert len(chunks[0].split()) >= planner.first_words


def test_connector_starts_new_breath_group():
    planner = SemanticChunkPlanner(min_words=6, max_words=40, first_words=3)
    # Past the minimum, a connector ("लेकिन") should start a new chunk.
    chunks = _feed_words(
        planner,
        "यह प्लान बहुत अच्छा है और सस्ता भी लेकिन इसमें कुछ शर्तें भी लागू होती हैं।",
    )
    # Some chunk after the first should begin with a connector.
    assert any(
        c.split()[0] in ("लेकिन", "और", "तो", "क्योंकि", "मतलब") for c in chunks[1:]
    ), chunks


def test_flush_returns_trailing_remainder():
    planner = SemanticChunkPlanner()
    assert planner.feed("बस थोड़ा सा") == []
    assert planner.flush() == "बस थोड़ा सा"
    # After flush, buffer is empty.
    assert planner.flush() == ""


# ── ProsodyShaper ─────────────────────────────────────────────────────────────


def test_shaper_keeps_devanagari():
    shaper = ProsodyShaper()
    out = shaper.shape("मैं आपकी मदद कर सकता हूँ")
    # No transliteration / mangling — Devanagari preserved.
    assert "मैं" in out and "हूँ" in out


def test_shaper_at_most_one_connector_per_turn():
    shaper = ProsodyShaper(max_injections_per_turn=1)
    shaper.reset_turn()
    outs = [
        shaper.shape("नमस्ते जी कैसे हैं आप"),  # idx 0 — never injected
        shaper.shape("यह प्लान आपके लिए बहुत फायदेमंद रहेगा"),
        shaper.shape("इसमें कोई छिपा हुआ चार्ज नहीं है बिल्कुल"),
        shaper.shape("तो आप कब शुरू करना चाहेंगे बताइए"),
    ]
    injected = sum(1 for o in outs if o.startswith("देखिए"))
    assert injected <= 1, outs


def test_shaper_no_bare_filler():
    shaper = ProsodyShaper()
    shaper.reset_turn()
    # A tiny chunk must never be turned into / preceded by a bare standalone filler.
    out = shaper.shape("हाँ")  # idx 0, also too short
    assert out == "हाँ"
    assert not out.startswith("देखिए،")


def test_shaper_does_not_double_marker():
    shaper = ProsodyShaper()
    shaper.reset_turn()
    shaper.shape("पहला वाक्य यहाँ पर है")  # idx 0 consumes first slot
    # A chunk already starting with a marker must not get another prepended.
    out = shaper.shape("तो आप क्या सोचते हैं इस बारे में जी")
    assert not out.startswith("देखिए")
    assert out.startswith("तो")


def test_shaper_normalizes_pauses():
    shaper = ProsodyShaper()
    shaper.reset_turn()
    out = shaper.shape("ठीक है...   चलिए\nआगे बढ़ते हैं")
    assert "..." not in out and "…" not in out
    assert "\n" not in out
    assert "   " not in out  # collapsed multi-space


def test_shaper_reset_restores_budget():
    shaper = ProsodyShaper(max_injections_per_turn=1)
    shaper.reset_turn()
    shaper.shape("पहला")  # idx 0
    shaper.shape("यह एक लंबा वाक्य है जिसमें कनेक्टर लग सकता है")  # may inject
    used_before = shaper._injections_used
    shaper.reset_turn()
    assert shaper._injections_used == 0 and shaper._chunk_index == 0
    assert used_before >= 0


# ── connector rotation (fixes "देखिए" every turn) ─────────────────────────────


def _inject_for_turn(connector: str) -> str:
    """Run a long Hindi chunk through a shaper for one turn; return the injected
    connector ("" if nothing was injected)."""
    shaper = ProsodyShaper(connector=connector)
    shaper.reset_turn()
    shaper.shape("नमस्ते जी कैसे हैं आप")  # idx 0 — never injected
    out = shaper.shape("यह प्लान आपके लिए बहुत फायदेमंद रहेगा सर बिल्कुल")
    for cand in ProsodyShaper.CONNECTORS:
        if out.startswith(cand + "،"):
            return cand
    return ""


def test_connector_not_injected_every_turn():
    """The field bug: "देखिए" appeared on turns 0,1,2 (every turn). With rare
    rotation, most turns must inject NOTHING."""
    last = None
    injected = []
    for t in range(9):
        c = next_connector(t, last)
        if c:
            last = c
        injected.append(_inject_for_turn(c))
    n_with = sum(1 for c in injected if c)
    # ~1 in 3 turns → at most 4 of 9, and strictly fewer than all turns.
    assert 1 <= n_with <= 4, injected
    assert n_with < 9, "connector injected on every turn (the regression)"


def test_connector_rotates_and_never_repeats_consecutively():
    """Across the turns that DO inject, the connector rotates and is never the
    same as the immediately preceding injected one."""
    last = None
    chosen = []
    for t in range(30):
        c = next_connector(t, last)
        if c:
            assert c in ProsodyShaper.CONNECTORS
            assert c != last, f"connector {c!r} repeated consecutively at turn {t}"
            last = c
            chosen.append(c)
    # Rotation actually visited more than one distinct connector.
    assert len(set(chosen)) >= 2, chosen


def test_shaper_uses_given_connector_not_hardcoded_dekhiye():
    """When a non-default connector is selected for the turn, it is the one
    injected — not the hardcoded देखिए."""
    got = _inject_for_turn("अच्छा")
    assert got == "अच्छा", got


def test_shaper_empty_connector_suppresses_injection():
    """connector="" means this turn injects nothing (the sparse/rare path)."""
    assert _inject_for_turn("") == ""
