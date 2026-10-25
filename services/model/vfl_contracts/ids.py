from __future__ import annotations

from decimal import Decimal, InvalidOperation
import re
from typing import NewType

SeasonId = NewType("SeasonId", str)
TournamentId = NewType("TournamentId", int)
MatchId = NewType("MatchId", int)
TeamUid = NewType("TeamUid", int)
DecimalOdds = NewType("DecimalOdds", str)

_DECIMAL_PATTERN = re.compile(r"^[0-9]+(?:\.[0-9]+)?$")


def _parse_namespaced_uint(raw: str, prefix: str, label: str) -> int:
    if not isinstance(raw, str) or not raw.startswith(prefix):
        raise ValueError(f"{label} must start with {prefix!r}: {raw!r}")
    suffix = raw[len(prefix) :]
    if not suffix.isdigit() or int(suffix) <= 0:
        raise ValueError(f"{label} has an invalid numeric suffix: {raw!r}")
    return int(suffix)


def canonical_match_id(raw: str) -> MatchId:
    return MatchId(_parse_namespaced_uint(raw, "vf:match:", "match ID"))


def canonical_team_uid(raw: str) -> TeamUid:
    return TeamUid(_parse_namespaced_uint(raw, "sr:competitor:", "team UID"))


def canonical_tournament_id(raw: str) -> TournamentId:
    return TournamentId(_parse_namespaced_uint(raw, "vf:tournament:", "tournament ID"))


def parse_decimal_odds(raw: str) -> DecimalOdds:
    """Validate positive decimal odds without converting them to binary float."""

    value = raw.strip() if isinstance(raw, str) else ""
    if not _DECIMAL_PATTERN.fullmatch(value):
        raise ValueError(f"invalid decimal odds: {raw!r}")
    try:
        decimal = Decimal(value)
    except InvalidOperation as exc:
        raise ValueError(f"invalid decimal odds: {raw!r}") from exc
    if not decimal.is_finite() or decimal <= 0:
        raise ValueError(f"odds must be finite and greater than zero: {raw!r}")
    return DecimalOdds(value)
