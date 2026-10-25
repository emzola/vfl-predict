from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import json
from math import isfinite
from pathlib import Path
from typing import Any, Iterable

import numpy as np


OUTCOMES = ("home", "draw", "away")
OUTCOME_INDEX = {name: index for index, name in enumerate(OUTCOMES)}
_UNVERIFIED_FEATURES = {
    "market_home_implied",
    "market_draw_implied",
    "market_away_implied",
    "market_overround",
}


def parse_timestamp(value: str) -> datetime:
    if not value:
        raise ValueError("timestamp is required")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError(f"timestamp must include timezone: {value!r}")
    return parsed


@dataclass(frozen=True)
class BookmakerLine:
    home: float
    draw: float
    away: float
    overround: float
    observed_at: datetime
    source_timestamp_at: datetime
    point_in_time_verified: bool
    observation_kind: str

    def probabilities(self) -> np.ndarray:
        return np.asarray([self.home, self.draw, self.away], dtype=np.float64)


@dataclass(frozen=True)
class DatasetRow:
    match_id: str
    season_id: str
    round_no: int
    scheduled_at: datetime
    cutoff_at: datetime
    home_team_uid: int
    away_team_uid: int
    actual: str
    features: dict[str, float]
    bookmaker: BookmakerLine | None
    baseline_probabilities: dict[str, np.ndarray]


@dataclass(frozen=True)
class Phase35Dataset:
    schema_version: str
    generated_at: datetime
    dataset_fingerprint: str
    feature_key: str
    feature_version: str
    odds_policy: str
    training_policy: str
    seasons: tuple[str, ...]
    season_starts: dict[str, datetime]
    season_ends: dict[str, datetime]
    rows: tuple[DatasetRow, ...]
    excluded_season_ids: tuple[str, ...]
    baseline_model_version: str

    @property
    def season_order(self) -> tuple[str, ...]:
        return self.seasons

    def rows_with_bookmaker(self) -> tuple[DatasetRow, ...]:
        return tuple(row for row in self.rows if row.bookmaker is not None)


def load_dataset(path: str | Path, require_bookmaker: bool = False) -> Phase35Dataset:
    document = json.loads(Path(path).read_text(encoding="utf-8"))
    if document.get("schema_version") != "phase35.dataset.v1":
        raise ValueError(f"unsupported dataset schema: {document.get('schema_version')!r}")
    fingerprint = str(document.get("dataset_fingerprint", ""))
    if len(fingerprint) != 64 or any(character not in "0123456789abcdef" for character in fingerprint):
        raise ValueError("dataset fingerprint must be a lowercase SHA-256 value")
    season_records = document.get("seasons") or []
    if not season_records:
        raise ValueError("dataset has no seasons")
    seasons: list[str] = []
    season_starts: dict[str, datetime] = {}
    season_ends: dict[str, datetime] = {}
    for record in season_records:
        season_id = str(record.get("season_id", ""))
        if not season_id or season_id in season_starts:
            raise ValueError(f"invalid or duplicate season: {season_id!r}")
        seasons.append(season_id)
        season_starts[season_id] = parse_timestamp(str(record.get("start_utc", "")))
        season_ends[season_id] = parse_timestamp(str(record.get("end_utc", "")))
    seasons.sort(key=lambda value: (season_starts[value], value))

    rows: list[DatasetRow] = []
    seen_matches: set[tuple[str, str]] = set()
    for raw in document.get("rows") or []:
        row = _parse_row(raw)
        key = (row.season_id, row.match_id)
        if key in seen_matches:
            raise ValueError(f"duplicate match row: {key}")
        if row.season_id not in season_starts:
            raise ValueError(f"row references unknown season: {row.season_id}")
        if require_bookmaker and row.bookmaker is None:
            continue
        seen_matches.add(key)
        rows.append(row)
    rows.sort(key=lambda row: (row.scheduled_at, row.match_id))
    if not rows:
        raise ValueError("dataset has no usable rows")
    return Phase35Dataset(
        schema_version="phase35.dataset.v1",
        generated_at=parse_timestamp(str(document.get("generated_at", ""))),
        dataset_fingerprint=fingerprint,
        feature_key=str(document.get("feature_key", "")),
        feature_version=str(document.get("feature_version", "")),
        odds_policy=str(document.get("odds_policy", "")),
        training_policy=str(document.get("training_policy", "")),
        seasons=tuple(seasons),
        season_starts=season_starts,
        season_ends=season_ends,
        rows=tuple(rows),
        excluded_season_ids=tuple(str(value) for value in document.get("excluded_incomplete_season_ids") or []),
        baseline_model_version=str(document.get("baseline_model_version", "")),
    )


def _parse_row(raw: dict[str, Any]) -> DatasetRow:
    match_id = str(raw.get("match_id", ""))
    season_id = str(raw.get("season_id", ""))
    actual = str(raw.get("actual", ""))
    if not match_id or not season_id or actual not in OUTCOME_INDEX:
        raise ValueError(f"invalid row identity/outcome: {match_id}/{season_id}/{actual}")
    features: dict[str, float] = {}
    for key, value in (raw.get("features") or {}).items():
        numeric = float(value)
        if not isfinite(numeric):
            raise ValueError(f"non-finite feature {key!r} on {match_id}")
        features[str(key)] = numeric
    if not features:
        raise ValueError(f"row has no features: {match_id}")
    bookmaker = _parse_bookmaker(raw.get("bookmaker"))
    baseline: dict[str, np.ndarray] = {}
    for model_name, values in (raw.get("baseline_probabilities") or {}).items():
        probability = _validate_probability(values, f"baseline {model_name} on {match_id}")
        baseline[str(model_name)] = probability
    return DatasetRow(
        match_id=match_id,
        season_id=season_id,
        round_no=int(raw.get("round", 0)),
        scheduled_at=parse_timestamp(str(raw.get("scheduled_at", ""))),
        cutoff_at=parse_timestamp(str(raw.get("cutoff_at", ""))),
        home_team_uid=int(raw.get("home_team_uid", 0)),
        away_team_uid=int(raw.get("away_team_uid", 0)),
        actual=actual,
        features=features,
        bookmaker=bookmaker,
        baseline_probabilities=baseline,
    )


def _parse_bookmaker(raw: Any) -> BookmakerLine | None:
    if raw is None:
        return None
    probability = _validate_probability(
        [raw.get("home"), raw.get("draw"), raw.get("away")], "bookmaker"
    )
    overround = float(raw.get("overround", 0.0))
    if not isfinite(overround) or overround <= 0:
        raise ValueError("bookmaker overround must be finite and positive")
    return BookmakerLine(
        home=float(probability[0]),
        draw=float(probability[1]),
        away=float(probability[2]),
        overround=overround,
        observed_at=parse_timestamp(str(raw.get("observed_at", ""))),
        source_timestamp_at=parse_timestamp(str(raw.get("source_timestamp_at", ""))),
        point_in_time_verified=bool(raw.get("point_in_time_verified", False)),
        observation_kind=str(raw.get("observation_kind", "")),
    )


def _validate_probability(values: Iterable[Any], label: str) -> np.ndarray:
    if isinstance(values, dict):
        try:
            values = (values["home"], values["draw"], values["away"])
        except KeyError as error:
            raise ValueError(f"{label} is missing an outcome probability") from error
    probability = np.asarray([float(value) for value in values], dtype=np.float64)
    if probability.shape != (3,) or not np.all(np.isfinite(probability)):
        raise ValueError(f"{label} must contain three finite probabilities")
    if np.any(probability < 0) or np.any(probability > 1):
        raise ValueError(f"{label} contains an out-of-range probability")
    if not np.isclose(float(probability.sum()), 1.0, atol=1e-7):
        raise ValueError(f"{label} does not sum to one: {probability.sum()}")
    return probability


def feature_names(rows: Iterable[DatasetRow], include_bookmaker: bool) -> tuple[str, ...]:
    names = {
        key
        for row in rows
        for key in row.features
        if key not in _UNVERIFIED_FEATURES
    }
    if include_bookmaker:
        names.update({"bookmaker_home", "bookmaker_draw", "bookmaker_away", "bookmaker_overround"})
    return tuple(sorted(names))


def feature_matrix(
    rows: Iterable[DatasetRow],
    include_bookmaker: bool,
    all_rows_for_categories: Iterable[DatasetRow] | None = None,
    category_values: dict[str, Iterable[float]] | None = None,
) -> tuple[np.ndarray, tuple[str, ...], tuple[int, ...]]:
    row_list = tuple(rows)
    category_rows = tuple(all_rows_for_categories or row_list)
    names = feature_names(category_rows, include_bookmaker)
    category_columns = tuple(
        index for index, name in enumerate(names) if name in {"home_team_uid", "away_team_uid"}
    )
    category_maps: dict[str, dict[float, int]] = {}
    for name in ("home_team_uid", "away_team_uid"):
        values = sorted(
            set(category_values[name]) if category_values and name in category_values else {
                row.features.get(name, 0.0) for row in category_rows
            }
        )
        # Reserve zero for an unseen category. This prevents a future-only
        # team from being silently treated as the first known team category.
        category_maps[name] = {value: index + 1 for index, value in enumerate(values)}
    matrix = np.zeros((len(row_list), len(names)), dtype=np.float64)
    for row_index, row in enumerate(row_list):
        for column, name in enumerate(names):
            if name == "bookmaker_home":
                value = row.bookmaker.home if row.bookmaker else 0.0
            elif name == "bookmaker_draw":
                value = row.bookmaker.draw if row.bookmaker else 0.0
            elif name == "bookmaker_away":
                value = row.bookmaker.away if row.bookmaker else 0.0
            elif name == "bookmaker_overround":
                value = row.bookmaker.overround if row.bookmaker else 0.0
            else:
                value = row.features.get(name, 0.0)
            if name in category_maps:
                value = float(category_maps[name].get(value, 0))
            if not isfinite(float(value)):
                raise ValueError(f"non-finite matrix value {name!r} on {row.match_id}")
            matrix[row_index, column] = float(value)
    return matrix, names, category_columns
