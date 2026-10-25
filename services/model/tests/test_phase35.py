from __future__ import annotations

from datetime import datetime, timedelta, timezone
from pathlib import Path
import unittest

import numpy as np

from services.model.vfl_lgbm.data import BookmakerLine, DatasetRow, _parse_bookmaker, feature_matrix
from services.model.vfl_lgbm.metrics import validate_probabilities
from services.model.vfl_lgbm.models import (
    LightGBMClassifier,
    LightGBMParams,
    fit_temperature,
    normalize_blend,
    residual_correct,
    temperature_scale,
)
from services.model.vfl_lgbm.pipeline import Phase35Config, _validation_folds


UTC = timezone.utc


def make_row(index: int, season_number: int) -> DatasetRow:
    scheduled = datetime(2020 + season_number, 1, 1, tzinfo=UTC) + timedelta(days=index)
    bookmaker = np.asarray([0.48, 0.27, 0.25], dtype=np.float64)
    return DatasetRow(
        match_id=str(season_number * 1000 + index),
        season_id=str(season_number),
        round_no=index + 1,
        scheduled_at=scheduled,
        cutoff_at=scheduled - timedelta(minutes=1),
        home_team_uid=(index % 4) + 1,
        away_team_uid=((index + 1) % 4) + 1,
        actual=("home", "draw", "away")[index % 3],
        features={
            "home_form": float((index * 3 + season_number) % 11) / 10.0,
            "away_form": float((index * 5 + season_number) % 13) / 12.0,
            "season_round": float(index + 1),
            "home_team_uid": float((index % 4) + 1),
            "away_team_uid": float(((index + 1) % 4) + 1),
        },
        bookmaker=BookmakerLine(
            home=float(bookmaker[0]),
            draw=float(bookmaker[1]),
            away=float(bookmaker[2]),
            overround=1.05,
            observed_at=scheduled - timedelta(minutes=2),
            source_timestamp_at=scheduled - timedelta(minutes=3),
            point_in_time_verified=False,
            observation_kind="retrospective_unverified_availability",
        ),
        baseline_probabilities={"bookmaker_implied": bookmaker},
    )


class Phase35ModelTests(unittest.TestCase):
    def test_probability_normalization_and_invalid_values(self) -> None:
        values = validate_probabilities(np.asarray([[2.0, 1.0, 1.0]]))
        np.testing.assert_allclose(values, [[0.5, 0.25, 0.25]])
        with self.assertRaises(ValueError):
            validate_probabilities(np.asarray([[np.nan, 0.5, 0.5]]))
        with self.assertRaises(ValueError):
            validate_probabilities(np.asarray([[0.0, 0.0, 0.0]]))

    def test_missing_and_invalid_bookmaker_lines_are_not_silently_accepted(self) -> None:
        row = make_row(0, 1)
        missing = DatasetRow(
            match_id=row.match_id,
            season_id=row.season_id,
            round_no=row.round_no,
            scheduled_at=row.scheduled_at,
            cutoff_at=row.cutoff_at,
            home_team_uid=row.home_team_uid,
            away_team_uid=row.away_team_uid,
            actual=row.actual,
            features=row.features,
            bookmaker=None,
            baseline_probabilities=row.baseline_probabilities,
        )
        matrix, names, _ = feature_matrix((missing,), True, (row, missing))
        bookmaker_columns = [names.index(name) for name in ("bookmaker_home", "bookmaker_draw", "bookmaker_away")]
        np.testing.assert_allclose(matrix[0, bookmaker_columns], 0.0)
        with self.assertRaises(ValueError):
            _parse_bookmaker({"home": 0.5, "draw": 0.25, "away": 0.25, "overround": 0.0})

    def test_residual_and_ensemble_preserve_probability_distribution(self) -> None:
        bookmaker = np.asarray([[0.5, 0.25, 0.25], [0.3, 0.3, 0.4]])
        model = np.asarray([[0.4, 0.4, 0.2], [0.2, 0.5, 0.3]])
        np.testing.assert_allclose(normalize_blend(model, bookmaker, 0.35).sum(axis=1), 1.0)
        np.testing.assert_allclose(residual_correct(bookmaker, model, 0.6).sum(axis=1), 1.0)

    def test_temperature_calibration_is_deterministic_and_valid(self) -> None:
        probabilities = np.asarray(
            [[0.7, 0.2, 0.1], [0.2, 0.6, 0.2], [0.1, 0.2, 0.7]], dtype=np.float64
        )
        labels = np.asarray([0, 1, 2])
        first = fit_temperature(probabilities, labels)
        second = fit_temperature(probabilities, labels)
        self.assertEqual(first, second)
        np.testing.assert_allclose(temperature_scale(probabilities, first).sum(axis=1), 1.0)

    def test_validation_folds_are_chronological_and_non_overlapping(self) -> None:
        seasons = tuple(str(value) for value in range(12))
        folds = _validation_folds(seasons, Phase35Config(minimum_training_seasons=5, validation_folds=4))
        self.assertGreaterEqual(len(folds), 2)
        for fold in folds:
            self.assertTrue(set(fold.train_seasons).isdisjoint(fold.validation_seasons))
            self.assertLess(max(seasons.index(value) for value in fold.train_seasons), min(seasons.index(value) for value in fold.validation_seasons))
        self.assertEqual(folds[-1].validation_seasons, ("11",))

    def test_lightgbm_is_reproducible_and_artifact_can_reload(self) -> None:
        rows = tuple(make_row(index, 1 + (index // 12)) for index in range(48))
        train, valid = rows[:36], rows[36:]
        params = LightGBMParams(
            num_leaves=7,
            max_depth=3,
            learning_rate=0.08,
            min_child_samples=2,
            n_estimators=20,
            early_stopping_rounds=5,
        )
        first = LightGBMClassifier(params, seed=123).fit(train, valid, False, rows)
        second = LightGBMClassifier(params, seed=123).fit(train, valid, False, rows)
        first_values = first.predict_proba(valid, False, rows)
        second_values = second.predict_proba(valid, False, rows)
        np.testing.assert_allclose(first_values, second_values, rtol=0, atol=1e-12)
        directory = Path("tmp") / "phase35-test-artifact"
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / "model.txt"
        try:
            first.save_model(str(path))
            loaded = LightGBMClassifier.load_model(
                str(path),
                first.feature_names,
                first.categorical_columns,
                first.category_values,
            )
            np.testing.assert_allclose(first_values, loaded.predict_proba(valid, False, rows), rtol=0, atol=1e-12)
        finally:
            if path.exists():
                path.unlink()
            if directory.exists():
                directory.rmdir()


if __name__ == "__main__":
    unittest.main()
