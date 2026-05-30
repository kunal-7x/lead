"""Unit tests for the prosody layer (SemanticChunkPlanner + ProsodyShaper).

ProsodyShaper is now a SAFETY NET only — no connector injection. Tests verify:
- Markdown/symbol stripping (stray LLM output never reaches TTS).
- Whitespace normalisation (line-breaks, multi-space, ellipsis).
- SemanticChunkPlanner unchanged (breath-group boundaries).
- next_connector always returns "" (no-op).
- Backward-compat: reset_turn / allow_inject / max_injections args don't crash.
"""

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
    joined = " ".join(chunks).split()
    assert joined == text.split(), "chunks lost/split words"
    for c in chunks:
        assert c == c.strip()
        for tok in c.split():
            assert tok in text.split() or tok.rstrip("।,.?!") in [
                w.rstrip("।,.?!") for w in text.split()
            ]


def test_respects_max_words_ceiling():
    planner = SemanticChunkPlanner(min_words=12, max_words=20, first_words=5)
    text = " ".join(f"शब्द{i}" for i in range(40))
    chunks = _feed_words(planner, text)
    assert len(chunks) >= 2
    for c in chunks[:-1]:
        assert len(c.split()) <= planner.max_words


def test_respects_min_words_holds_short_clause():
    planner = SemanticChunkPlanner(min_words=12, max_words=35, first_words=5)
    chunks = _feed_words(
        planner,
        "अच्छा ठीक है। हाँ। मैं आपको पूरी जानकारी विस्तार से देता हूँ अभी इसी समय।",
    )
    assert "हाँ।" not in chunks
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
    assert len(chunks[0].split()) < planner.min_words
    assert len(chunks[0].split()) >= planner.first_words


def test_connector_starts_new_breath_group():
    planner = SemanticChunkPlanner(min_words=6, max_words=40, first_words=3)
    chunks = _feed_words(
        planner,
        "यह प्लान बहुत अच्छा है और सस्ता भी लेकिन इसमें कुछ शर्तें भी लागू होती हैं।",
    )
    assert any(
        c.split()[0] in ("लेकिन", "और", "तो", "क्योंकि", "मतलब") for c in chunks[1:]
    ), chunks


def test_flush_returns_trailing_remainder():
    planner = SemanticChunkPlanner()
    assert planner.feed("बस थोड़ा सा") == []
    assert planner.flush() == "बस थोड़ा सा"
    assert planner.flush() == ""


# ── ProsodyShaper: safety-net markdown stripping ──────────────────────────────


def test_shaper_keeps_devanagari():
    shaper = ProsodyShaper()
    out = shaper.shape("मैं आपकी मदद कर सकता हूँ")
    assert "मैं" in out and "हूँ" in out


def test_shaper_strips_bold():
    shaper = ProsodyShaper()
    out = shaper.shape("**यह एक bold टेक्स्ट है**")
    assert "**" not in out
    assert "यह एक bold टेक्स्ट है" in out


def test_shaper_strips_italic():
    shaper = ProsodyShaper()
    out = shaper.shape("*italic text* here")
    assert "*" not in out


def test_shaper_strips_heading_markers():
    shaper = ProsodyShaper()
    out = shaper.shape("## Section Header")
    assert "#" not in out
    assert "Section Header" in out


def test_shaper_strips_bullet_markers():
    shaper = ProsodyShaper()
    out = shaper.shape("- यह एक bullet point है")
    assert "- " not in out.lstrip()
    assert "यह" in out


def test_shaper_strips_pipe_and_arrow():
    shaper = ProsodyShaper()
    out = shaper.shape("option A | option B → select")
    assert "|" not in out
    assert "→" not in out


def test_shaper_strips_backtick():
    shaper = ProsodyShaper()
    out = shaper.shape("use `budget` field")
    assert "`" not in out
    assert "budget" in out


def test_shaper_no_connector_injection():
    """Safety-net shaper must NOT inject connectors — LLM handles prosody now."""
    shaper = ProsodyShaper(max_injections_per_turn=1)
    shaper.reset_turn()
    shaper.shape("नमस्ते जी कैसे हैं आप")  # idx 0
    out = shaper.shape("यह प्लान आपके लिए बहुत फायदेमंद रहेगा")
    # Must NOT start with any connector from the old set
    for connector in ProsodyShaper.CONNECTORS:
        assert not out.startswith(connector), f"connector '{connector}' injected: {out!r}"


def test_shaper_normalizes_pauses():
    shaper = ProsodyShaper()
    out = shaper.shape("ठीक है...   चलिए\nआगे बढ़ते हैं")
    assert "..." not in out and "…" not in out
    assert "\n" not in out
    assert "   " not in out


def test_shaper_reset_turn_compat():
    """reset_turn() must not crash (backward-compat)."""
    shaper = ProsodyShaper(max_injections_per_turn=1)
    shaper.reset_turn()
    shaper.shape("पहला")
    shaper.reset_turn()
    assert shaper._chunk_index == 0


def test_shaper_allow_inject_param_compat():
    """allow_inject param must not crash (backward-compat, no effect now)."""
    shaper = ProsodyShaper()
    out = shaper.shape("यह टेक्स्ट है", allow_inject=False)
    assert "यह" in out
    out2 = shaper.shape("और यह भी", allow_inject=True)
    assert "और" in out2


# ── next_connector always returns "" ──────────────────────────────────────────


def test_next_connector_always_empty():
    """next_connector is a no-op — connector injection moved to LLM."""
    last = None
    for t in range(12):
        c = next_connector(t, last)
        assert c == "", f"turn {t}: expected '' got {c!r}"
        last = c or last
