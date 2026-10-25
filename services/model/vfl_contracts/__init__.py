"""Typed Phase 0 provider and canonical contract definitions.

This package intentionally contains no prediction or inference code.
"""

from .ids import (
    DecimalOdds,
    MatchId,
    SeasonId,
    TeamUid,
    TournamentId,
    canonical_match_id,
    canonical_team_uid,
    canonical_tournament_id,
    parse_decimal_odds,
)

__all__ = [
    "DecimalOdds",
    "MatchId",
    "SeasonId",
    "TeamUid",
    "TournamentId",
    "canonical_match_id",
    "canonical_team_uid",
    "canonical_tournament_id",
    "parse_decimal_odds",
]
