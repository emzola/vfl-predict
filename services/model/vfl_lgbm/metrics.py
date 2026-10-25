from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from typing import Iterable

import numpy as np

from .data import OUTCOME_INDEX, OUTCOMES, DatasetRow


@dataclass(frozen=True)
class Metrics:
    count: int
    log_loss: float
    brier: float
    accuracy: float
    ece: float

    def as_dict(self) -> dict[str, float | int]:
        return {
            "count": self.count,
            "log_loss": self.log_loss,
            "multiclass_brier": self.brier,
            "accuracy": self.accuracy,
            "expected_calibration_error": self.ece,
        }


def validate_probabilities(probabilities: np.ndarray, tolerance: float = 1e-7) -> np.ndarray:
    values = np.asarray(probabilities, dtype=np.float64)
    if values.ndim != 2 or values.shape[1] != 3:
        raise ValueError(f"probabilities must have shape (n, 3), got {values.shape}")
    if not np.all(np.isfinite(values)) or np.any(values < -tolerance):
        raise ValueError("probabilities must be finite and nonnegative")
    clipped = np.clip(values, 0.0, None)
    totals = clipped.sum(axis=1, keepdims=True)
    if np.any(totals <= 0):
        raise ValueError("probability rows must have positive mass")
    normalized = clipped / totals
    if not np.allclose(normalized.sum(axis=1), 1.0, atol=tolerance):
        raise ValueError("probability rows must sum to one")
    return normalized


def labels_for(rows: Iterable[DatasetRow]) -> np.ndarray:
    return np.asarray([OUTCOME_INDEX[row.actual] for row in rows], dtype=np.int64)


def compute_metrics(rows: Iterable[DatasetRow], probabilities: np.ndarray) -> Metrics:
    row_list = tuple(rows)
    if len(row_list) != len(probabilities):
        raise ValueError("row/probability lengths differ")
    if not row_list:
        return Metrics(0, 0.0, 0.0, 0.0, 0.0)
    probabilities = validate_probabilities(probabilities)
    labels = labels_for(row_list)
    selected = np.clip(probabilities[np.arange(len(labels)), labels], 1e-15, 1.0)
    one_hot = np.zeros_like(probabilities)
    one_hot[np.arange(len(labels)), labels] = 1.0
    brier = float(np.mean(np.sum((probabilities - one_hot) ** 2, axis=1)))
    accuracy = float(np.mean(np.argmax(probabilities, axis=1) == labels))
    return Metrics(
        count=len(row_list),
        log_loss=float(-np.mean(np.log(selected))),
        brier=brier,
        accuracy=accuracy,
        ece=expected_calibration_error(labels, probabilities),
    )


def expected_calibration_error(labels: np.ndarray, probabilities: np.ndarray, bins: int = 10) -> float:
    values = validate_probabilities(probabilities)
    labels = np.asarray(labels, dtype=np.int64)
    if len(labels) == 0:
        return 0.0
    total = 0.0
    for class_index in range(3):
        predicted = values[:, class_index]
        observed = (labels == class_index).astype(np.float64)
        for lower, upper in zip(np.linspace(0.0, 1.0, bins + 1)[:-1], np.linspace(0.0, 1.0, bins + 1)[1:]):
            mask = (predicted >= lower) & ((predicted < upper) if upper < 1.0 else (predicted <= upper))
            if not np.any(mask):
                continue
            total += float(mask.mean()) * abs(float(predicted[mask].mean()) - float(observed[mask].mean()))
    return total / 3.0


def reliability(labels: np.ndarray, probabilities: np.ndarray, bins: int = 10) -> list[dict[str, float | int | str]]:
    values = validate_probabilities(probabilities)
    result: list[dict[str, float | int | str]] = []
    edges = np.linspace(0.0, 1.0, bins + 1)
    for class_index, class_name in enumerate(OUTCOMES):
        predicted = values[:, class_index]
        observed = (labels == class_index).astype(np.float64)
        for bin_index, (lower, upper) in enumerate(zip(edges[:-1], edges[1:])):
            mask = (predicted >= lower) & ((predicted < upper) if upper < 1.0 else (predicted <= upper))
            if not np.any(mask):
                continue
            result.append(
                {
                    "class": class_name,
                    "bin": bin_index,
                    "count": int(mask.sum()),
                    "mean_predicted": float(predicted[mask].mean()),
                    "observed_frequency": float(observed[mask].mean()),
                }
            )
    return result


def by_season(rows: Iterable[DatasetRow], probabilities: np.ndarray) -> dict[str, Metrics]:
    row_list = tuple(rows)
    result: dict[str, Metrics] = {}
    for season_id in sorted({row.season_id for row in row_list}):
        indices = [index for index, row in enumerate(row_list) if row.season_id == season_id]
        result[season_id] = compute_metrics([row_list[index] for index in indices], probabilities[indices])
    return result


def by_actual_outcome(rows: Iterable[DatasetRow], probabilities: np.ndarray) -> dict[str, dict[str, float | int]]:
    row_list = tuple(rows)
    values = validate_probabilities(probabilities)
    labels = labels_for(row_list)
    result: dict[str, dict[str, float | int]] = {}
    for outcome, outcome_index in OUTCOME_INDEX.items():
        mask = labels == outcome_index
        count = int(mask.sum())
        if count == 0:
            result[outcome] = {"count": 0, "mean_probability": 0.0, "accuracy": 0.0, "log_loss": 0.0}
            continue
        selected = np.clip(values[mask, outcome_index], 1e-15, 1.0)
        result[outcome] = {
            "count": count,
            "mean_probability": float(values[mask, outcome_index].mean()),
            "accuracy": float(np.mean(np.argmax(values[mask], axis=1) == outcome_index)),
            "log_loss": float(-np.mean(np.log(selected))),
        }
    return result


def block_bootstrap_metric(
    rows: Iterable[DatasetRow],
    probabilities: np.ndarray,
    seed: int,
    replicates: int = 400,
) -> dict[str, dict[str, float | int]]:
    row_list = tuple(rows)
    if not row_list or replicates <= 0:
        return {}
    groups: dict[str, list[int]] = defaultdict(list)
    for index, row in enumerate(row_list):
        groups[row.season_id].append(index)
    seasons = sorted(groups)
    rng = np.random.default_rng(seed)
    samples: dict[str, list[float]] = {"log_loss": [], "brier": [], "accuracy": []}
    for _ in range(replicates):
        selected_indices = [index for season in rng.choice(seasons, size=len(seasons), replace=True) for index in groups[str(season)]]
        metrics = compute_metrics(
            [row_list[index] for index in selected_indices], probabilities[selected_indices]
        )
        samples["log_loss"].append(metrics.log_loss)
        samples["brier"].append(metrics.brier)
        samples["accuracy"].append(metrics.accuracy)
    return {
        key: {
            "low": float(np.quantile(values, 0.025)),
            "high": float(np.quantile(values, 0.975)),
            "replicates": replicates,
        }
        for key, values in samples.items()
    }


def paired_block_bootstrap_difference(
    rows: Iterable[DatasetRow],
    candidate: np.ndarray,
    benchmark: np.ndarray,
    seed: int,
    replicates: int = 400,
) -> dict[str, dict[str, float | int]]:
    row_list = tuple(rows)
    if len(row_list) != len(candidate) or len(row_list) != len(benchmark):
        raise ValueError("paired comparison lengths differ")
    groups: dict[str, list[int]] = defaultdict(list)
    for index, row in enumerate(row_list):
        groups[row.season_id].append(index)
    seasons = sorted(groups)
    rng = np.random.default_rng(seed)
    differences: dict[str, list[float]] = {"log_loss": [], "brier": [], "accuracy": []}
    for _ in range(replicates):
        selected = [index for season in rng.choice(seasons, size=len(seasons), replace=True) for index in groups[str(season)]]
        selected_rows = [row_list[index] for index in selected]
        candidate_metrics = compute_metrics(selected_rows, candidate[selected])
        benchmark_metrics = compute_metrics(selected_rows, benchmark[selected])
        differences["log_loss"].append(candidate_metrics.log_loss - benchmark_metrics.log_loss)
        differences["brier"].append(candidate_metrics.brier - benchmark_metrics.brier)
        differences["accuracy"].append(candidate_metrics.accuracy - benchmark_metrics.accuracy)
    return {
        key: {
            "low": float(np.quantile(values, 0.025)),
            "high": float(np.quantile(values, 0.975)),
            "replicates": replicates,
        }
        for key, values in differences.items()
    }
