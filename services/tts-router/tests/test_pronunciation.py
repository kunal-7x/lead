from __future__ import annotations

from tts_router.pronunciation import normalize
from tests.conftest import make_req


def test_indian_number_expansion():
    result = normalize("Price is 60L")
    assert "sixty lakh" in result.lower()


def test_crore_expansion():
    assert "two crore" in normalize("Budget is 2cr").lower()


def test_currency_expansion():
    result = normalize("EMI is ₹15,000").lower()
    assert "rupees" in result


def test_phone_number_expansion():
    result = normalize("Call 9876543210 for details")
    assert "9" in result and " " in result


def test_date_expansion():
    result = normalize("Possession on 15/08/2025")
    assert "August" in result
    assert "two thousand" in result or "2025" in result


def test_no_change_plain_text():
    text = "Namaste aapka swagat hai"
    assert normalize(text) == text


def test_overrides_applied():
    result = normalize("BHK flat available", overrides={"BHK": "bedroom hall kitchen"})
    assert "bedroom hall kitchen" in result


async def test_indian_number_expansion_in_router(router):
    """'60L' in text → audio returned (synthesis didn't fail)."""
    req = make_req(text="Price is 60L only")
    result = await router.synthesize(req)
    assert result.tier_used == "sarvam_bulbul"
    assert len(result.audio) > 0


def test_lakh_float():
    assert "crore" in normalize("property costs 1.2cr").lower()


def test_num_words_basic():
    from tts_router.pronunciation import _num_words
    assert _num_words(1) == "one"
    assert _num_words(15) == "fifteen"
    assert _num_words(60) == "sixty"
    assert _num_words(100) == "one hundred"
