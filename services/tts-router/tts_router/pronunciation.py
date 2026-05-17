from __future__ import annotations

import re

# Indian number expansion rules applied before synthesis
_LAKH_RE = re.compile(r"(\d+(?:\.\d+)?)\s*[lL](?:akh)?", re.IGNORECASE)
_CRORE_RE = re.compile(r"(\d+(?:\.\d+)?)\s*[cC]r(?:ore)?", re.IGNORECASE)
_CURRENCY_RE = re.compile(r"₹\s?(\d[\d,]*)", )
_PHONE_RE = re.compile(r"\b([6-9]\d{9})\b")
_DATE_RE = re.compile(r"\b(\d{1,2})/(\d{1,2})/(\d{4})\b")

_MONTHS = ["", "January", "February", "March", "April", "May", "June",
           "July", "August", "September", "October", "November", "December"]

_ONES = ["", "one", "two", "three", "four", "five", "six", "seven",
         "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen",
         "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"]
_TENS = ["", "", "twenty", "thirty", "forty", "fifty",
         "sixty", "seventy", "eighty", "ninety"]


def normalize(text: str, overrides: dict[str, str] | None = None) -> str:
    """Expand Indian numbers, currency, phone numbers, and dates into spoken form."""
    if overrides:
        for term, phonetic in overrides.items():
            text = text.replace(term, phonetic)

    text = _LAKH_RE.sub(_expand_lakh, text)
    text = _CRORE_RE.sub(_expand_crore, text)
    text = _CURRENCY_RE.sub(_expand_currency, text)
    text = _PHONE_RE.sub(_expand_phone, text)
    text = _DATE_RE.sub(_expand_date, text)
    return text


def _expand_lakh(m: re.Match) -> str:
    val = m.group(1)
    if "." in val:
        return f"{val} lakh"
    n = int(float(val))
    return f"{_num_words(n)} lakh"


def _expand_crore(m: re.Match) -> str:
    val = m.group(1)
    if "." in val:
        return f"{val} crore"
    n = int(float(val))
    return f"{_num_words(n)} crore"


def _expand_currency(m: re.Match) -> str:
    digits = m.group(1).replace(",", "")
    try:
        n = int(digits)
    except ValueError:
        return m.group(0)
    return f"{_num_words(n)} rupees"


def _expand_phone(m: re.Match) -> str:
    return " ".join(m.group(1))


def _expand_date(m: re.Match) -> str:
    day, month, year = int(m.group(1)), int(m.group(2)), int(m.group(3))
    month_name = _MONTHS[month] if 1 <= month <= 12 else str(month)
    return f"{_ordinal(day)} {month_name} {_year_words(year)}"


def _num_words(n: int) -> str:
    if n == 0:
        return "zero"
    if n < 20:
        return _ONES[n]
    if n < 100:
        return _TENS[n // 10] + (" " + _ONES[n % 10] if n % 10 else "")
    if n < 1000:
        rest = _num_words(n % 100) if n % 100 else ""
        return _ONES[n // 100] + " hundred" + (" " + rest if rest else "")
    return str(n)


def _ordinal(n: int) -> str:
    if 11 <= n <= 13:
        return f"{n}th"
    return {1: f"{n}st", 2: f"{n}nd", 3: f"{n}rd"}.get(n % 10, f"{n}th")


def _year_words(y: int) -> str:
    if 2000 <= y <= 2099:
        rest = y - 2000
        return "two thousand" + (" " + _num_words(rest) if rest else "")
    return str(y)
