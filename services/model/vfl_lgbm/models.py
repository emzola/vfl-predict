from __future__ import annotations

from dataclasses import asdict, dataclass
from typing import Iterable

import lightgbm as lgb
import numpy as np

from .data import DatasetRow, feature_matrix
from .metrics import compute_metrics, validate_probabilities


@dataclass(frozen=True)
class LightGBMParams:
    num_leaves: int = 15
    max_depth: int = 5
    learning_rate: float = 0.05
    min_child_samples: int = 30
    feature_fraction: float = 0.9
    bagging_fraction: float = 0.9
    lambda_l1: float = 0.1
    lambda_l2: float = 2.0
    n_estimators: int = 300
    early_stopping_rounds: int = 35

    def as_dict(self) -> dict[str, int | float]:
        return asdict(self)


def bounded_search_space(limit: int = 4) -> tuple[LightGBMParams, ...]:
    candidates = (
        LightGBMParams(num_leaves=7, max_depth=3, learning_rate=0.04, min_child_samples=40, feature_fraction=0.8, bagging_fraction=0.8, lambda_l1=0.2, lambda_l2=5.0),
        LightGBMParams(num_leaves=15, max_depth=5, learning_rate=0.05, min_child_samples=30, feature_fraction=0.9, bagging_fraction=0.9, lambda_l1=0.1, lambda_l2=2.0),
        LightGBMParams(num_leaves=31, max_depth=7, learning_rate=0.03, min_child_samples=50, feature_fraction=0.8, bagging_fraction=0.8, lambda_l1=0.5, lambda_l2=8.0),
        LightGBMParams(num_leaves=15, max_depth=5, learning_rate=0.08, min_child_samples=60, feature_fraction=1.0, bagging_fraction=1.0, lambda_l1=1.0, lambda_l2=10.0),
    )
    if limit <= 0:
        raise ValueError("search limit must be positive")
    return candidates[: min(limit, len(candidates))]


class LightGBMClassifier:
    def __init__(self, params: LightGBMParams, seed: int) -> None:
        self.params = params
        self.seed = seed
        self.model: lgb.Booster | None = None
        self.feature_names: tuple[str, ...] = ()
        self.categorical_columns: tuple[int, ...] = ()
        self.category_values: dict[str, tuple[float, ...]] = {}
        self.best_iteration: int = params.n_estimators

    def fit(self, train_rows: Iterable[DatasetRow], valid_rows: Iterable[DatasetRow] | None, include_bookmaker: bool, all_rows_for_categories: Iterable[DatasetRow]) -> "LightGBMClassifier":
        train = tuple(train_rows)
        valid = tuple(valid_rows or ())
        if not train:
            raise ValueError("LightGBM training set is empty")
        train_matrix, names, categorical = feature_matrix(train, include_bookmaker, train)
        self.category_values = {
            name: tuple(sorted({row.features.get(name, 0.0) for row in train}))
            for name in ("home_team_uid", "away_team_uid")
        }
        labels = np.asarray([{"home": 0, "draw": 1, "away": 2}[row.actual] for row in train], dtype=np.int32)
        estimator_count = self.params.n_estimators
        train_set = lgb.Dataset(
            train_matrix,
            label=labels,
            feature_name=list(names),
            categorical_feature=list(categorical),
            free_raw_data=False,
        )
        parameters: dict[str, object] = {
            "objective": "multiclass",
            "num_class": 3,
            "num_leaves": self.params.num_leaves,
            "max_depth": self.params.max_depth,
            "learning_rate": self.params.learning_rate,
            "min_child_samples": self.params.min_child_samples,
            "feature_fraction": self.params.feature_fraction,
            "bagging_fraction": self.params.bagging_fraction,
            "bagging_freq": 1 if self.params.bagging_fraction < 1.0 else 0,
            "lambda_l1": self.params.lambda_l1,
            "lambda_l2": self.params.lambda_l2,
            "seed": self.seed,
            "feature_fraction_seed": self.seed,
            "bagging_seed": self.seed,
            "data_random_seed": self.seed,
            "deterministic": True,
            "force_col_wise": True,
            "verbosity": -1,
            "num_threads": 1,
        }
        valid_sets = None
        valid_names = None
        callbacks = None
        if valid:
            valid_matrix, _, _ = feature_matrix(
                valid,
                include_bookmaker,
                train,
                self.category_values,
            )
            valid_labels = np.asarray([{"home": 0, "draw": 1, "away": 2}[row.actual] for row in valid], dtype=np.int32)
            valid_set = lgb.Dataset(valid_matrix, label=valid_labels, reference=train_set, free_raw_data=False)
            valid_sets = [valid_set]
            valid_names = ["validation"]
            callbacks = [
                lgb.early_stopping(self.params.early_stopping_rounds, verbose=False),
                lgb.log_evaluation(0),
            ]
        model = lgb.train(
            parameters,
            train_set,
            num_boost_round=estimator_count,
            valid_sets=valid_sets,
            valid_names=valid_names,
            callbacks=callbacks,
        )
        self.model = model
        self.feature_names = names
        self.categorical_columns = categorical
        self.best_iteration = int(model.best_iteration or estimator_count)
        return self

    def predict_proba(self, rows: Iterable[DatasetRow], include_bookmaker: bool, all_rows_for_categories: Iterable[DatasetRow]) -> np.ndarray:
        if self.model is None:
            raise RuntimeError("model is not fitted")
        row_list = tuple(rows)
        category_rows = all_rows_for_categories
        matrix, names, _ = feature_matrix(
            row_list,
            include_bookmaker,
            category_rows,
            self.category_values or None,
        )
        if names != self.feature_names:
            raise ValueError("feature schema changed after fit")
        predicted = self.model.predict(matrix)
        return validate_probabilities(np.asarray(predicted, dtype=np.float64))

    def save_model(self, path: str) -> None:
        if self.model is None:
            raise RuntimeError("model is not fitted")
        self.model.save_model(path)

    @classmethod
    def load_model(
        cls,
        path: str,
        feature_names: Iterable[str],
        categorical_columns: Iterable[int] = (),
        category_values: dict[str, Iterable[float]] | None = None,
    ) -> "LightGBMClassifier":
        """Load an immutable text artifact written by :meth:`save_model`.

        Metadata is supplied separately so the loader cannot silently accept a
        changed feature schema. The registry checksum and metadata file are
        validated by the surrounding artifact workflow.
        """
        booster = lgb.Booster(model_file=path)
        instance = cls(
            LightGBMParams(n_estimators=max(1, int(booster.current_iteration()))),
            seed=0,
        )
        instance.model = booster
        instance.feature_names = tuple(feature_names)
        instance.categorical_columns = tuple(categorical_columns)
        instance.category_values = {
            name: tuple(values) for name, values in (category_values or {}).items()
        }
        instance.best_iteration = max(1, int(booster.current_iteration()))
        return instance


def temperature_scale(probabilities: np.ndarray, temperature: float) -> np.ndarray:
    values = validate_probabilities(probabilities)
    if temperature <= 0 or not np.isfinite(temperature):
        raise ValueError("temperature must be finite and positive")
    logits = np.log(np.clip(values, 1e-15, 1.0)) / temperature
    logits -= logits.max(axis=1, keepdims=True)
    scaled = np.exp(logits)
    return validate_probabilities(scaled)


def fit_temperature(probabilities: np.ndarray, labels: np.ndarray) -> float:
    values = validate_probabilities(probabilities)
    labels = np.asarray(labels, dtype=np.int64)
    if len(values) != len(labels) or len(values) == 0:
        raise ValueError("calibration data is empty or mismatched")
    # A deterministic bounded grid avoids adding an optimizer dependency and
    # keeps calibration reproducible on the small VPS target.
    best_temperature = 1.0
    best_loss = float("inf")
    for temperature in np.exp(np.linspace(np.log(0.25), np.log(4.0), 161)):
        scaled = temperature_scale(values, float(temperature))
        selected = np.clip(scaled[np.arange(len(labels)), labels], 1e-15, 1.0)
        loss = float(-np.mean(np.log(selected)))
        if loss < best_loss - 1e-12:
            best_loss = loss
            best_temperature = float(temperature)
    return best_temperature


def choose_weight(rows: Iterable[DatasetRow], model_probabilities: np.ndarray, seed: int) -> float:
    row_list = tuple(rows)
    bookmaker = np.asarray([row.bookmaker.probabilities() for row in row_list], dtype=np.float64)
    best_weight = 0.0
    best_loss = float("inf")
    for weight in np.linspace(0.0, 1.0, 21):
        combined = normalize_blend(model_probabilities, bookmaker, float(weight))
        loss = compute_metrics(row_list, combined).log_loss
        if loss < best_loss - 1e-12:
            best_loss = loss
            best_weight = float(weight)
    return best_weight


def choose_residual_strength(rows: Iterable[DatasetRow], model_probabilities: np.ndarray) -> float:
    row_list = tuple(rows)
    bookmaker = np.asarray([row.bookmaker.probabilities() for row in row_list], dtype=np.float64)
    best_strength = 0.0
    best_loss = float("inf")
    for strength in np.linspace(0.0, 1.0, 21):
        corrected = residual_correct(bookmaker, model_probabilities, float(strength))
        loss = compute_metrics(row_list, corrected).log_loss
        if loss < best_loss - 1e-12:
            best_loss = loss
            best_strength = float(strength)
    return best_strength


def normalize_blend(model_probabilities: np.ndarray, bookmaker: np.ndarray, weight: float) -> np.ndarray:
    model = validate_probabilities(model_probabilities)
    market = validate_probabilities(bookmaker)
    return validate_probabilities(weight * model + (1.0 - weight) * market)


def residual_correct(bookmaker: np.ndarray, model_probabilities: np.ndarray, strength: float) -> np.ndarray:
    market = validate_probabilities(bookmaker)
    model = validate_probabilities(model_probabilities)
    # Log-space residual correction: the LightGBM distribution is compared to
    # a uniform prior, then applied multiplicatively to the bookmaker line.
    residual = np.log(np.clip(model, 1e-15, 1.0)) - np.log(1.0 / 3.0)
    return validate_probabilities(market * np.exp(float(strength) * residual))


def model_search_summary(params: LightGBMParams, average_log_loss: float, folds: int) -> dict[str, object]:
    return {"hyperparameters": params.as_dict(), "average_validation_log_loss": average_log_loss, "folds": folds}
