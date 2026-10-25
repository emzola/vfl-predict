from __future__ import annotations

from typing import Literal, NotRequired, TypedDict

from .ids import DecimalOdds, MatchId, SeasonId, TeamUid, TournamentId


class ProviderTime(TypedDict):
    date: str
    time: str
    tz: str
    tzoffset: int
    uts: int


class ProviderTeam(TypedDict):
    _id: int
    uid: int
    name: str
    mediumname: str
    abbr: str
    nickname: NotRequired[str | None]


class ProviderResult(TypedDict):
    home: int | None
    away: int | None
    period: NotRequired[str | None]
    winner: NotRequired[str | None]


class ProviderScore(TypedDict):
    home: int
    away: int


class ProviderPeriodsObject(TypedDict, total=False):
    p1: ProviderScore
    ft: ProviderScore


ProviderPeriods = ProviderPeriodsObject | list[object] | None


class ProviderFixtureTeams(TypedDict):
    home: ProviderTeam
    away: ProviderTeam


class ProviderFixture(TypedDict):
    _id: int
    _seasonid: int
    round: int
    time: ProviderTime
    teams: ProviderFixtureTeams
    result: ProviderResult
    periods: ProviderPeriods
    postponed: bool
    canceled: bool
    walkover: bool
    status: NotRequired[object | None]


class ProviderFixtureData(TypedDict):
    _id: str
    _utid: int
    start: ProviderTime
    end: ProviderTime
    matches: list[ProviderFixture]


class ProviderFixtureDoc(TypedDict):
    data: ProviderFixtureData
    _dob: NotRequired[int]
    _maxage: NotRequired[int]
    _pages: NotRequired[int]


class ProviderFixtureEnvelope(TypedDict):
    queryUrl: str
    doc: list[ProviderFixtureDoc]


class ProviderOddsTeam(TypedDict):
    id: str
    name: str
    abbr: str


class ProviderOutcome(TypedDict):
    id: str
    name: str
    abbr: str
    odds: str
    active: int
    result: int


class ProviderMarket(TypedDict):
    id: int
    name: str
    specifiers: NotRequired[str | None]
    status: int
    favourite: NotRequired[int | None]
    outcome: list[ProviderOutcome]


class ProviderOddsTeams(TypedDict):
    home: ProviderOddsTeam
    away: ProviderOddsTeam


class ProviderOddsRecord(TypedDict):
    id: str
    sport_id: str
    tournament_id: str
    teams: ProviderOddsTeams
    market: list[ProviderMarket]


class ProviderOddsData(TypedDict):
    odds: list[ProviderOddsRecord]


class ProviderOddsDoc(TypedDict):
    data: ProviderOddsData
    timestamp: int


class ProviderOddsEnvelope(TypedDict):
    queryUrl: str
    doc: list[ProviderOddsDoc]


class ProviderSeasonMetaSeason(TypedDict):
    _id: str | int
    _utid: int
    name: str
    abbr: str
    currentseasonid: str | int
    start: ProviderTime
    end: ProviderTime
    year: str | int


class ProviderSeasonMetaData(TypedDict):
    season: ProviderSeasonMetaSeason
    tournamentids: list[int]
    tableids: list[int]
    cupids: list[int]
    statscoverage: dict[str, bool]


class ProviderSeasonMetaEnvelope(TypedDict):
    queryUrl: str
    doc: list[TypedDict("ProviderSeasonMetaDoc", {"data": ProviderSeasonMetaData})]


class ProviderPositionHistoryData(TypedDict):
    teamcount: int
    roundcount: int
    teams: dict[str, ProviderTeam]
    positiondata: list[object]
    matchesmissing: list[object]


class ProviderPositionHistoryEnvelope(TypedDict):
    queryUrl: str
    doc: list[TypedDict("ProviderPositionHistoryDoc", {"data": ProviderPositionHistoryData})]


class ProviderLiveTableRow(TypedDict):
    _id: int
    team: ProviderTeam
    pos: int
    total: int
    pointsTotal: int
    goalsForTotal: int
    goalsAgainstTotal: int


class ProviderLiveTableData(TypedDict):
    _id: str
    seasonid: str
    currentround: int
    maxrounds: int
    totalrows: int
    tablerows: list[ProviderLiveTableRow]


class ProviderLiveTableEnvelope(TypedDict):
    queryUrl: str
    doc: list[TypedDict("ProviderLiveTableDoc", {"data": ProviderLiveTableData})]


class CanonicalFixture(TypedDict):
    match_id: MatchId
    season_id: SeasonId
    tournament_id: TournamentId
    round: int
    scheduled_uts: int
    home_team_uid: TeamUid
    away_team_uid: TeamUid
    home_score: int | None
    away_score: int | None
    status: Literal["scheduled", "in_progress", "completed", "canceled", "postponed", "unknown"]


class CanonicalOutcome(TypedDict):
    outcome_id: str
    odds: DecimalOdds | None
    raw_odds: str
    active: int
    provider_result: int


class CanonicalMarketIdentity(TypedDict):
    match_id: MatchId
    market_id: int
    specifiers: str
    outcome_id: str
