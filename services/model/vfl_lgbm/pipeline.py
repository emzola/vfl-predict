from __future__ import annotations

from dataclasses import dataclass, replace
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
from typing import Any, Iterable

import numpy as np

from .data import DatasetRow, Phase35Dataset, feature_matrix
from .metrics import (
    Metrics,
    block_bootstrap_metric,
    by_actual_outcome,
    by_season,
    compute_metrics,
    labels_for,
    paired_block_bootstrap_difference,
    reliability,
    validate_probabilities,
)
from .models import (
    LightGBMClassifier,
    LightGBMParams,
    bounded_search_space,
    choose_residual_strength,
    choose_weight,
    fit_temperature,
    normalize_blend,
    residual_correct,
    temperature_scale,
)


@dataclass(frozen=True)
class Phase35Config:
    test_seasons: int = 3
    minimum_training_seasons: int = 8
    validation_folds: int = 4
    search_candidates: int = 4
    bootstrap_replicates: int = 400
    seed: int = 20261025
    model_version: str = "phase35.v1"
    artifact_dir: str = "artifacts/models/phase35"
    code_version: str = "phase35-python-2026-10-25"


@dataclass(frozen=True)
class ValidationFold:
    index: int
    train_seasons: tuple[str, ...]
    validation_seasons: tuple[str, ...]


@dataclass
class FittedOOF:
    rows: tuple[DatasetRow, ...]
    probabilities: np.ndarray
    folds: list[dict[str, Any]]


def run_phase35(dataset: Phase35Dataset, config: Phase35Config | None = None) -> dict[str, Any]:
    config = config or Phase35Config()
    if config.test_seasons < 1:
        raise ValueError("test_seasons must be positive")
    if len(dataset.season_order) <= config.test_seasons + config.minimum_training_seasons:
        raise ValueError("dataset does not contain enough seasons for development and final test sets")
    common_rows = tuple(row for row in dataset.rows if row.bookmaker is not None)
    if not common_rows:
        raise ValueError("no rows have a valid bookmaker line")
    final_test_seasons = tuple(dataset.season_order[-config.test_seasons :])
    development_seasons = tuple(dataset.season_order[: -config.test_seasons])
    final_test_rows = _rows_for_seasons(common_rows, final_test_seasons)
    folds = _validation_folds(development_seasons, config)
    search_space = bounded_search_space(config.search_candidates)

    no_odds_search = _search_lightgbm(
        common_rows, folds, search_space, include_bookmaker=False, config=config, candidate_key="lightgbm_without_bookmaker"
    )
    with_odds_search = _search_lightgbm(
        common_rows, folds, search_space, include_bookmaker=True, config=config, candidate_key="lightgbm_with_bookmaker"
    )
    no_params = no_odds_search["selected_params"]
    with_params = with_odds_search["selected_params"]
    no_oof = _collect_oof(common_rows, folds, no_params, False, config, candidate_key="lightgbm_without_bookmaker")
    with_oof = _collect_oof(common_rows, folds, with_params, True, config, candidate_key="lightgbm_with_bookmaker")
    if [row.match_id for row in no_oof.rows] != [row.match_id for row in with_oof.rows]:
        raise AssertionError("LightGBM OOF rows differ between feature variants")

    no_model, no_iterations = _fit_final(
        common_rows,
        development_seasons,
        no_params,
        no_odds_search["selected_best_iteration"],
        False,
        config,
    )
    with_model, with_iterations = _fit_final(
        common_rows,
        development_seasons,
        with_params,
        with_odds_search["selected_best_iteration"],
        True,
        config,
    )
    no_test = no_model.predict_proba(final_test_rows, False, common_rows)
    with_test = with_model.predict_proba(final_test_rows, True, common_rows)

    bookmaker_oof = _baseline_probabilities(no_oof.rows, "bookmaker_implied")
    bookmaker_test = _baseline_probabilities(final_test_rows, "bookmaker_implied")
    poisson_oof = _baseline_probabilities(no_oof.rows, "poisson")
    poisson_test = _baseline_probabilities(final_test_rows, "poisson")
    combination_oof = _baseline_probabilities(no_oof.rows, "statistical_bookmaker_combination")
    combination_test = _baseline_probabilities(final_test_rows, "statistical_bookmaker_combination")
    residual_strength = choose_residual_strength(no_oof.rows, no_oof.probabilities)
    ensemble_weight = choose_weight(no_oof.rows, no_oof.probabilities, config.seed)
    bookmaker_oof_values = np.asarray([row.bookmaker.probabilities() for row in no_oof.rows], dtype=np.float64)
    bookmaker_test_values = np.asarray([row.bookmaker.probabilities() for row in final_test_rows], dtype=np.float64)
    residual_oof = residual_correct(bookmaker_oof_values, no_oof.probabilities, residual_strength)
    residual_test = residual_correct(bookmaker_test_values, no_test, residual_strength)
    ensemble_oof = normalize_blend(no_oof.probabilities, bookmaker_oof_values, ensemble_weight)
    ensemble_test = normalize_blend(no_test, bookmaker_test_values, ensemble_weight)

    raw_predictions = {
        "poisson": (poisson_oof, poisson_test),
        "bookmaker_implied": (bookmaker_oof, bookmaker_test),
        "statistical_bookmaker_combination": (combination_oof, combination_test),
        "lightgbm_without_bookmaker": (no_oof.probabilities, no_test),
        "lightgbm_with_bookmaker": (with_oof.probabilities, with_test),
        "bookmaker_residual_correction": (residual_oof, residual_test),
        "lightgbm_bookmaker_ensemble": (ensemble_oof, ensemble_test),
    }
    calibrated_temperatures: dict[str, float] = {}
    calibrated_test: dict[str, np.ndarray] = {}
    for name, (oof, test) in raw_predictions.items():
        temperature = fit_temperature(oof, labels_for(no_oof.rows))
        calibrated_temperatures[name] = temperature
        calibrated_test[name] = temperature_scale(test, temperature)

    benchmark = raw_predictions["bookmaker_implied"][1]
    candidates: dict[str, Any] = {}
    for name, (oof, test) in raw_predictions.items():
        candidate = {
            "raw": _evaluation(final_test_rows, test, benchmark, config, name),
            "calibrated": _evaluation(final_test_rows, calibrated_test[name], benchmark, config, name),
            "calibration": {
                "temperature": calibrated_temperatures[name],
                "raw": compute_metrics(final_test_rows, test).as_dict(),
                "calibrated": compute_metrics(final_test_rows, calibrated_test[name]).as_dict(),
            },
            "validation": compute_metrics(no_oof.rows, oof).as_dict(),
        }
        candidates[name] = candidate

    artifact_records, artifact_manifest = _write_artifacts(
        dataset,
        config,
        no_model,
        with_model,
        no_iterations,
        with_iterations,
        no_params,
        with_params,
        calibrated_temperatures,
        residual_strength,
        ensemble_weight,
        candidates,
    )
    experiments = _experiment_manifest(dataset, config, no_odds_search, with_odds_search)
    experiments.extend(_research_experiment_manifest(dataset, config, candidates, calibrated_temperatures, residual_strength, ensemble_weight))
    report: dict[str, Any] = {
        "schema_version": "phase35.report.v1",
        "generated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "dataset_fingerprint": dataset.dataset_fingerprint,
        "odds_policy": dataset.odds_policy,
        "training_policy": dataset.training_policy,
        "season_count": len(dataset.season_order),
        "row_count": len(dataset.rows),
        "common_eligible_row_count": len(common_rows),
        "development_seasons": list(development_seasons),
        "final_test_seasons": list(final_test_seasons),
        "final_test_row_count": len(final_test_rows),
        "excluded_incomplete_season_ids": list(dataset.excluded_season_ids),
        "validation_folds": [
            {"index": fold.index, "train_seasons": list(fold.train_seasons), "validation_seasons": list(fold.validation_seasons)}
            for fold in folds
        ],
        "search": {
            "lightgbm_without_bookmaker": no_odds_search,
            "lightgbm_with_bookmaker": with_odds_search,
        },
        "post_processing": {
            "residual_strength": residual_strength,
            "ensemble_lightgbm_weight": ensemble_weight,
            "definition": "normalize(bookmaker * exp(strength * (log(lightgbm_probability) - log(1/3))))",
        },
        "explainability": {
            "lightgbm_without_bookmaker": _explainability(no_oof.folds),
            "lightgbm_with_bookmaker": _explainability(with_oof.folds),
            "interpretation": "gain importance is a diagnostic, not evidence of causality",
        },
        "models": candidates,
        "artifacts": artifact_records,
        "experiments": experiments,
        "promotion_gates": _promotion_gates(candidates),
        "research_limitations": [
            "historical odds are retrospective_unverified_availability unless the export policy says otherwise",
            "final test seasons are untouched by hyperparameter and calibration fitting",
            "positive ROI is not a promotion criterion",
        ],
    }
    manifest = {
        "schema_version": "phase35.manifest.v1",
        "generated_at": report["generated_at"],
        "dataset_fingerprint": dataset.dataset_fingerprint,
        "artifacts": artifact_manifest,
        "experiments": experiments,
    }
    report["manifest"] = manifest
    return report


def _validation_folds(seasons: tuple[str, ...], config: Phase35Config) -> tuple[ValidationFold, ...]:
    if config.minimum_training_seasons >= len(seasons):
        raise ValueError("minimum training seasons leaves no validation season")
    indices = np.linspace(config.minimum_training_seasons, len(seasons) - 1, config.validation_folds, dtype=int)
    unique_indices = tuple(dict.fromkeys(int(value) for value in indices))
    return tuple(
        ValidationFold(index=index, train_seasons=seasons[:index], validation_seasons=(seasons[index],))
        for index in unique_indices
    )


def _rows_for_seasons(rows: Iterable[DatasetRow], seasons: Iterable[str]) -> tuple[DatasetRow, ...]:
    allowed = set(seasons)
    return tuple(row for row in rows if row.season_id in allowed)


def _search_lightgbm(
    rows: tuple[DatasetRow, ...],
    folds: tuple[ValidationFold, ...],
    search_space: tuple[LightGBMParams, ...],
    include_bookmaker: bool,
    config: Phase35Config,
    candidate_key: str,
) -> dict[str, Any]:
    experiments: list[dict[str, Any]] = []
    best_params = search_space[0]
    selected_best_iteration = search_space[0].n_estimators
    selected_candidate_index = 0
    best_score = float("inf")
    for candidate_index, params in enumerate(search_space):
        fold_metrics: list[dict[str, Any]] = []
        iterations: list[int] = []
        for fold in folds:
            train = _rows_for_seasons(rows, fold.train_seasons)
            valid = _rows_for_seasons(rows, fold.validation_seasons)
            model = LightGBMClassifier(params, config.seed + candidate_index * 100 + fold.index)
            model.fit(train, valid, include_bookmaker, rows)
            probabilities = model.predict_proba(valid, include_bookmaker, rows)
            fold_metrics.append(compute_metrics(valid, probabilities).as_dict())
            iterations.append(model.best_iteration)
        average = float(np.mean([metric["log_loss"] for metric in fold_metrics]))
        experiments.append({"candidate_index": candidate_index, "hyperparameters": params.as_dict(), "fold_metrics": fold_metrics, "average_validation_log_loss": average, "best_iterations": iterations})
        if average < best_score - 1e-12:
            best_score = average
            best_params = params
            selected_candidate_index = candidate_index
            selected_best_iteration = int(
                round(float(np.median(experiments[-1]["best_iterations"])))
            )
    return {
        "selected_params": best_params.as_dict(),
        "selected_candidate_index": selected_candidate_index,
        "selected_best_iteration": max(1, selected_best_iteration),
        "selected_average_validation_log_loss": best_score,
        "experiments": experiments,
    }


def _collect_oof(
    rows: tuple[DatasetRow, ...],
    folds: tuple[ValidationFold, ...],
    params_dict: dict[str, Any],
    include_bookmaker: bool,
    config: Phase35Config,
    candidate_key: str,
) -> FittedOOF:
    params = LightGBMParams(**params_dict)
    all_oof_rows: list[DatasetRow] = []
    all_probabilities: list[np.ndarray] = []
    diagnostics: list[dict[str, Any]] = []
    for fold in folds:
        train = _rows_for_seasons(rows, fold.train_seasons)
        valid = _rows_for_seasons(rows, fold.validation_seasons)
        model = LightGBMClassifier(params, config.seed + 10_000 + fold.index)
        model.fit(train, valid, include_bookmaker, rows)
        probability = model.predict_proba(valid, include_bookmaker, rows)
        gains = model.model.feature_importance(importance_type="gain") if model.model else np.zeros(len(model.feature_names))
        diagnostics.append({"fold": fold.index, "best_iteration": model.best_iteration, "feature_names": list(model.feature_names), "feature_gain": [float(value) for value in gains], "metrics": compute_metrics(valid, probability).as_dict(), "candidate_key": candidate_key})
        all_oof_rows.extend(valid)
        all_probabilities.append(probability)
    return FittedOOF(tuple(all_oof_rows), np.vstack(all_probabilities), diagnostics)


def _fit_final(
    rows: tuple[DatasetRow, ...],
    development_seasons: tuple[str, ...],
    params_dict: dict[str, Any],
    selected_best_iteration: int,
    include_bookmaker: bool,
    config: Phase35Config,
) -> tuple[LightGBMClassifier, int]:
    params = replace(
        LightGBMParams(**params_dict),
        n_estimators=max(1, min(int(selected_best_iteration), int(params_dict["n_estimators"]))),
    )
    train = _rows_for_seasons(rows, development_seasons)
    # Early-stopping iterations are selected in rolling validation. The final
    # fit uses their bounded median without consulting the final test block.
    model = LightGBMClassifier(params, config.seed + (2_000 if include_bookmaker else 1_000))
    model.fit(train, None, include_bookmaker, rows)
    return model, model.best_iteration


def _explainability(folds: list[dict[str, Any]]) -> dict[str, Any]:
    if not folds:
        return {"folds": [], "feature_stability": []}
    names = tuple(folds[0]["feature_names"])
    gains = np.asarray([fold["feature_gain"] for fold in folds], dtype=np.float64)
    totals = gains.sum(axis=1, keepdims=True)
    normalized = np.divide(gains, totals, out=np.zeros_like(gains), where=totals > 0)
    stability = []
    for index, name in enumerate(names):
        values = normalized[:, index]
        stability.append(
            {
                "feature": name,
                "mean_normalized_gain": float(values.mean()),
                "std_normalized_gain": float(values.std()),
                "folds_with_nonzero_gain": int(np.count_nonzero(gains[:, index] > 0)),
            }
        )
    stability.sort(key=lambda item: (-item["mean_normalized_gain"], item["feature"]))
    return {
        "folds": folds,
        "feature_stability": stability,
    }


def _baseline_probabilities(rows: tuple[DatasetRow, ...], model_name: str) -> np.ndarray:
    values: list[np.ndarray] = []
    for row in rows:
        if model_name not in row.baseline_probabilities:
            raise ValueError(f"missing {model_name} baseline for {row.season_id}/{row.match_id}")
        values.append(row.baseline_probabilities[model_name])
    return validate_probabilities(np.vstack(values))


def _evaluation(rows: tuple[DatasetRow, ...], probabilities: np.ndarray, benchmark: np.ndarray, config: Phase35Config, name: str) -> dict[str, Any]:
    metrics = compute_metrics(rows, probabilities)
    benchmark_metrics = compute_metrics(rows, benchmark)
    return {
        "metrics": metrics.as_dict(),
        "by_season": {key: value.as_dict() for key, value in by_season(rows, probabilities).items()},
        "by_actual_outcome": by_actual_outcome(rows, probabilities),
        "reliability": reliability(labels_for(rows), probabilities),
        "confidence_intervals": block_bootstrap_metric(rows, probabilities, config.seed + 30_000, config.bootstrap_replicates),
        "paired_difference_vs_bookmaker": paired_block_bootstrap_difference(rows, probabilities, benchmark, config.seed + 40_000 + len(name), config.bootstrap_replicates),
        "benchmark_metrics": benchmark_metrics.as_dict(),
    }


def _promotion_gates(candidates: dict[str, Any]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    benchmark = candidates["bookmaker_implied"]["raw"]["metrics"]
    for name, candidate in candidates.items():
        raw_metrics = candidate["raw"]["metrics"]
        calibrated_metrics = candidate["calibrated"]["metrics"]
        difference = candidate["raw"]["paired_difference_vs_bookmaker"]
        raw_improves = raw_metrics["log_loss"] < benchmark["log_loss"] and raw_metrics["multiclass_brier"] < benchmark["multiclass_brier"]
        calibrated_improves = calibrated_metrics["log_loss"] < benchmark["log_loss"] and calibrated_metrics["multiclass_brier"] < benchmark["multiclass_brier"]
        interval_support = difference["log_loss"]["high"] < 0 and difference["brier"]["high"] < 0
        result[name] = {
            "eligible_for_approval": bool(name != "bookmaker_implied" and (raw_improves or calibrated_improves) and interval_support),
            "out_of_sample_improvement_required": True,
            "raw_improves_primary_metrics": bool(raw_improves),
            "calibrated_improves_primary_metrics": bool(calibrated_improves),
            "paired_interval_supports_improvement": bool(interval_support),
            "promotion_status": "candidate",
        }
    return result


def _write_artifacts(
    dataset: Phase35Dataset,
    config: Phase35Config,
    no_model: LightGBMClassifier,
    with_model: LightGBMClassifier,
    no_iterations: int,
    with_iterations: int,
    no_params: dict[str, Any],
    with_params: dict[str, Any],
    temperatures: dict[str, float],
    residual_strength: float,
    ensemble_weight: float,
    candidates: dict[str, Any],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    root = Path(config.artifact_dir)
    root.mkdir(parents=True, exist_ok=True)
    training_cutoff = max(dataset.season_ends[season] for season in dataset.season_order[:-config.test_seasons])
    records: list[dict[str, Any]] = []
    manifest: list[dict[str, Any]] = []

    def write_model(candidate_key: str, model: LightGBMClassifier, params: dict[str, Any], iteration: int) -> None:
        model_dir = root / candidate_key
        model_dir.mkdir(parents=True, exist_ok=True)
        model_path = model_dir / f"{config.model_version}.txt"
        model.save_model(str(model_path))
        checksum = _sha256(model_path)
        metadata = {
            "candidate_key": candidate_key,
            "model_version": config.model_version,
            "dataset_fingerprint": dataset.dataset_fingerprint,
            "feature_schema_version": f"{dataset.feature_key}:{dataset.feature_version}:phase35",
            "feature_names": list(model.feature_names),
            "categorical_columns": list(model.categorical_columns),
            "category_values": {key: list(values) for key, values in model.category_values.items()},
            "hyperparameters": params,
            "best_iteration": iteration,
            "training_cutoff": training_cutoff.isoformat().replace("+00:00", "Z"),
            "random_seed": config.seed,
            "calibration_config": {"temperature": temperatures[candidate_key]},
            "evaluation_metrics": candidates[candidate_key],
            "artifact_sha256": checksum,
        }
        metadata_path = model_dir / f"{config.model_version}.metadata.json"
        metadata_path.write_text(json.dumps(metadata, indent=2, sort_keys=True), encoding="utf-8")
        record = {
            "model_key": candidate_key,
            "version": config.model_version,
            "description": f"Phase 3.5 LightGBM research artifact: {candidate_key}",
            "algorithm": "lightgbm",
            "artifact_path": str(model_path),
            "artifact_sha256": checksum,
            "feature_schema_version": metadata["feature_schema_version"],
            "training_cutoff": metadata["training_cutoff"],
            "dataset_fingerprint": dataset.dataset_fingerprint,
            "hyperparameters": params,
            "calibration_config": metadata["calibration_config"],
            "evaluation_metrics": candidates[candidate_key],
            "random_seed": config.seed,
        }
        records.append(record)
        manifest.append(record)

    write_model("lightgbm_without_bookmaker", no_model, no_params, no_iterations)
    write_model("lightgbm_with_bookmaker", with_model, with_params, with_iterations)
    for candidate_key, post_process in (
        ("bookmaker_residual_correction", {"residual_strength": residual_strength}),
        ("lightgbm_bookmaker_ensemble", {"lightgbm_weight": ensemble_weight}),
    ):
        path = root / candidate_key / f"{config.model_version}.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({"candidate_key": candidate_key, **post_process}, indent=2, sort_keys=True), encoding="utf-8")
        checksum = _sha256(path)
        record = {
            "model_key": candidate_key,
            "version": config.model_version,
            "description": f"Phase 3.5 post-processing artifact: {candidate_key}",
            "algorithm": "lightgbm",
            "artifact_path": str(path),
            "artifact_sha256": checksum,
            "feature_schema_version": f"{dataset.feature_key}:{dataset.feature_version}:phase35",
            "training_cutoff": training_cutoff.isoformat().replace("+00:00", "Z"),
            "dataset_fingerprint": dataset.dataset_fingerprint,
            "hyperparameters": post_process,
            "calibration_config": {"temperature": temperatures[candidate_key]},
            "evaluation_metrics": candidates[candidate_key],
            "random_seed": config.seed,
        }
        records.append(record)
        manifest.append(record)
    return records, manifest


def _experiment_manifest(dataset: Phase35Dataset, config: Phase35Config, *searches: dict[str, Any]) -> list[dict[str, Any]]:
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    experiments: list[dict[str, Any]] = []
    for search_index, search in enumerate(searches):
        candidate_key = (
            "lightgbm_without_bookmaker"
            if search_index == 0
            else "lightgbm_with_bookmaker"
        )
        for experiment in search["experiments"]:
            experiments.append({
                "experiment_key": f"{dataset.dataset_fingerprint[:12]}:{candidate_key}:{experiment['candidate_index']}",
                "dataset_fingerprint": dataset.dataset_fingerprint,
                "candidate_key": candidate_key,
                "algorithm": "lightgbm",
                "fold_configuration": {"validation_folds": len(experiment["fold_metrics"]), "chronological": True},
                "hyperparameters": experiment["hyperparameters"],
                "metrics": {"fold_metrics": experiment["fold_metrics"], "average_validation_log_loss": experiment["average_validation_log_loss"]},
                "random_seed": config.seed,
                "status": "completed",
                "started_at": now,
                "completed_at": now,
                "code_version": config.code_version,
            })
    return experiments


def _research_experiment_manifest(
    dataset: Phase35Dataset,
    config: Phase35Config,
    candidates: dict[str, Any],
    temperatures: dict[str, float],
    residual_strength: float,
    ensemble_weight: float,
) -> list[dict[str, Any]]:
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    experiments: list[dict[str, Any]] = []

    def append_experiment(candidate_key: str, algorithm: str, hyperparameters: dict[str, Any], metrics: dict[str, Any]) -> None:
        experiments.append(
            {
                "experiment_key": f"{dataset.dataset_fingerprint[:12]}:{candidate_key}:{algorithm}",
                "dataset_fingerprint": dataset.dataset_fingerprint,
                "candidate_key": candidate_key,
                "algorithm": algorithm,
                "fold_configuration": {"chronological": True, "final_test_untouched": True},
                "hyperparameters": hyperparameters,
                "metrics": metrics,
                "random_seed": config.seed,
                "status": "completed",
                "started_at": now,
                "completed_at": now,
                "code_version": config.code_version,
            }
        )

    for candidate_key in ("poisson", "bookmaker_implied", "statistical_bookmaker_combination"):
        append_experiment(candidate_key, "baseline", {}, candidates[candidate_key])
    for candidate_key, temperature in temperatures.items():
        append_experiment(
            candidate_key,
            "calibration",
            {"method": "temperature_grid", "temperature": temperature},
            candidates[candidate_key]["calibration"],
        )
    append_experiment(
        "bookmaker_residual_correction",
        "ensemble",
        {"residual_strength": residual_strength},
        candidates["bookmaker_residual_correction"],
    )
    append_experiment(
        "lightgbm_bookmaker_ensemble",
        "ensemble",
        {"lightgbm_weight": ensemble_weight},
        candidates["lightgbm_bookmaker_ensemble"],
    )
    return experiments


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_report(report: dict[str, Any], report_path: str | Path, manifest_path: str | Path | None = None) -> None:
    report_file = Path(report_path)
    report_file.parent.mkdir(parents=True, exist_ok=True)
    report_file.write_text(json.dumps(report, indent=2, sort_keys=True), encoding="utf-8")
    if manifest_path is not None:
        manifest_file = Path(manifest_path)
        manifest_file.parent.mkdir(parents=True, exist_ok=True)
        manifest_file.write_text(json.dumps(report["manifest"], indent=2, sort_keys=True), encoding="utf-8")
