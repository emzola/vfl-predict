from __future__ import annotations

import argparse
from pathlib import Path

from .data import load_dataset
from .pipeline import Phase35Config, run_phase35, write_report


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Run the controlled, chronological Phase 3.5 LightGBM research evaluation."
    )
    parser.add_argument("--dataset", default="tmp/phase35-dataset.json")
    parser.add_argument("--output-report", default="tmp/phase35-lgbm-report.json")
    parser.add_argument("--output-manifest", default="tmp/phase35-manifest.json")
    parser.add_argument("--artifacts-dir", default="artifacts/models/phase35")
    parser.add_argument("--test-seasons", type=int, default=3)
    parser.add_argument("--minimum-training-seasons", type=int, default=8)
    parser.add_argument("--validation-folds", type=int, default=4)
    parser.add_argument("--search-candidates", type=int, default=4)
    parser.add_argument("--bootstrap-replicates", type=int, default=400)
    parser.add_argument("--seed", type=int, default=20261025)
    return parser


def main() -> int:
    args = build_parser().parse_args()
    dataset = load_dataset(args.dataset, require_bookmaker=True)
    report = run_phase35(
        dataset,
        Phase35Config(
            test_seasons=args.test_seasons,
            minimum_training_seasons=args.minimum_training_seasons,
            validation_folds=args.validation_folds,
            search_candidates=args.search_candidates,
            bootstrap_replicates=args.bootstrap_replicates,
            seed=args.seed,
            artifact_dir=args.artifacts_dir,
        ),
    )
    write_report(report, args.output_report, args.output_manifest)
    gates = report["promotion_gates"]
    eligible = [name for name, value in gates.items() if value["eligible_for_approval"]]
    print(
        "phase35_report={} seasons={} eligible_rows={} final_test_seasons={} "
        "final_test_rows={} promotion_eligible={}".format(
            Path(args.output_report),
            report["season_count"],
            report["common_eligible_row_count"],
            len(report["final_test_seasons"]),
            report["final_test_row_count"],
            ",".join(eligible) if eligible else "none",
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
