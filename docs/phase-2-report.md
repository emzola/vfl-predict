# Phase 2 implementation and verification report

Date: 2026-10-25

Phase 2 stopped after the requested controlled batch. It does not add
prediction scheduling, authentication, a dashboard, automated betting, or
LightGBM/XGBoost training.

## Completed implementation

- Added additive migration `003_phase2_research.sql` for versioned feature
  definitions/snapshots, model definitions, training runs, backtest runs,
  historical predictions, evaluation metrics, and research-only betting
  analysis records.
- Added `services/research`, a dependency-light deterministic Go package with:
  - strict pre-cutoff feature generation for results, home/away rates, rolling
    3/5/10-match form and goals, head-to-head, previous-season statistics,
    as-of standings, rest, and verified odds/overround when available;
  - overall and home/draw/away frequency benchmarks, team-strength and
    Poisson goal models, bookmaker-implied probabilities with overround removal,
    and a weighted statistical/bookmaker combination;
  - walk-forward season/round backtesting, log loss, multiclass Brier score,
    accuracy, ECE, reliability data, and season-block bootstrap intervals;
  - research-only expected-value and flat-stake calculations with ROI,
    hit-rate, return, drawdown, and season slices.
- Added database loading and optional persistence of feature, prediction,
  model, backtest, and evaluation artifacts. The dataset loader excludes
  structurally incomplete seasons from research evaluation while retaining them
  in the ingestion database and coverage reports.
- Updated automatic ingestion selection to reuse seasons with 30 complete
  rounds and eight joined odds match records per round. The default remains one
  season and the per-run cap remains ten.
- Added `cmd/phase2-evaluate`, `cmd/phase2-verify`, and
  `scripts/verify-phase2.ps1`.

## Controlled live validation and ingestion

The live read-only discovery request selected two uncovered completed seasons:
`3149394` and `3149369`. The existing complete `3149425` season was reused.
No other historical season was requested.

| Season | Fixtures | Rounds | Fixtures/round | Odds match records/round | Usable outcomes | Invalid/quarantined outcomes | Join failures |
|---|---:|---:|---:|---:|---:|---:|---:|
| 3149425 (Phase 1, reused) | 240 | 30 | 8 | 8 | 33,582 | 30 | 0 |
| 3149394 (Phase 2) | 240 | 30 | 8 | 8 | 33,588 | 30 | 0 |
| 3149369 (Phase 2) | 240 | 30 | 8 | 8 | 33,586 | 30 | 0 |

The controlled batch therefore contains three structurally complete seasons,
720 fixtures, 90 source payload timestamps, and 90 invalid `0.00` outcome rows
retained/quarantined. The Phase 2 database query found no odds-to-fixture or
team-ID join failures. All imported odds are stored as
`historical_returned_after_settlement` with `point_in_time_verified=false`.

The live responses confirmed 240 final-score fixtures, 30 rounds, eight odds
records per round, 16 stable team UIDs, and matching normalized fixture/odds
IDs. Across the controlled seasons, market statuses were `-3`, outcomes were
inactive (`active=0`), and the 1X2 provider-result values observed were `0` and
`1`. Market `1` was named `1x2`; its outcomes used IDs `1`, `2`, and `3`, with
the draw identified by the label `draw` and team outcomes identified by team
labels. The adapter does not assume fixed provider outcome IDs; it requires a
complete unambiguous line.

These findings narrow the source contract but do not establish market-status
semantics, original odds publication time, opening/closing price semantics, or
tradable historical availability. See the updated [API assumptions register](api-assumptions.md).

## Point-in-time policy

The feature builder uses the earliest scheduled kickoff in the target round
minus a 30-second safety buffer. It admits only final results whose scheduled
time is strictly before that cutoff and excludes same-round results. Future
result mutation tests pass. Historical odds are included in point-in-time
features only when both `point_in_time_verified=true` and an explicit observed
timestamp is at or before the cutoff. The controlled imported odds do not meet
that policy, so the market-implied feature group is recorded as unavailable.

The evaluator has an explicit `-allow-retrospective-odds` switch. The reported
bookmaker comparison uses that opt-in retrospective track and is not evidence
of a historical trading opportunity.

## Dataset and model evaluation

The final evaluator used three complete imported seasons (720 final matches),
held out seasons `3149394` and `3149425`, and 480 predictions per model. The
first complete season, `3149369`, supplied the initial training history.
One-fixture integration-test seasons remained in PostgreSQL but were excluded
from research evaluation as incomplete; they are test artifacts, not imported
historical seasons.

| Model | Log loss | Multiclass Brier | Accuracy | ECE | 95% block-bootstrap log-loss interval |
|---|---:|---:|---:|---:|---:|
| Overall frequency | 1.0640 | 0.6434 | 0.4563 | 0.0293 | 1.0481–1.0799 |
| Home/draw/away frequency | 1.0640 | 0.6434 | 0.4563 | 0.0293 | 1.0481–1.0799 |
| Team strength | 1.0843 | 0.6393 | 0.5229 | 0.1014 | 1.0593–1.1092 |
| Poisson | 1.0028 | 0.5962 | 0.5250 | 0.0306 | 0.9816–1.0239 |
| Bookmaker implied, overround removed | 0.9755 | 0.5796 | 0.5417 | 0.0230 | 0.9648–0.9862 |
| Statistical/bookmaker combination | 0.9803 | 0.5831 | 0.5292 | 0.0346 | 0.9669–0.9938 |

On this small retrospective sample, bookmaker-implied probabilities were best
on log loss, Brier score, accuracy, and ECE among the reported baselines. No
statistical model outperformed the bookmaker baseline on the primary proper
scoring metrics. This is an observation about this dataset, not a general
claim.

The combination model did not improve the bookmaker baseline. Reliability
data and season/matchday slices are included in `tmp/phase2-report.json` when
the evaluator is run; confidence intervals resample whole seasons to avoid
treating correlated within-season fixtures as independent observations.

## Research-only betting analysis

Using the combination model and the explicit retrospective odds opt-in:

| Selections | Hit rate | Stake | Return | Profit | ROI | Maximum drawdown |
|---:|---:|---:|---:|---:|---:|---:|
| 193 | 30.57% | 193.00 | 162.80 | -30.20 | -15.65% | 45.55 |

Season results were `3149394`: 108 selections, -22.50% ROI; and `3149425`:
85 selections, -6.94% ROI. These are hypothetical flat-stake arithmetic only.
No bets were placed and no bankroll or execution path exists.

## Verification

Offline verification passed with two synthetic seasons, 32 fixtures, 32 feature
vectors, 96 baseline predictions, and 16 research-only selections:

```powershell
go run ./cmd/phase2-verify
```

The Phase 2 test suite covers cutoff leakage, same-round exclusion,
season-boundary behavior, stable IDs, invalid/unverified odds, probability
normalization, Poisson outputs, deterministic walk-forward splits and
reproduction, bootstrap metrics, ROI arithmetic, and the Phase 2 PostgreSQL
schema/storage integration test when `PHASE2_DATABASE_URL` is set. Run the full
verification with:

```powershell
./scripts/verify-phase2.ps1
```

The final local database verification also applied all migrations, loaded 720
complete fixtures, generated 720 feature snapshots, persisted a backtest run,
and persisted model predictions/metrics. Credentials were not added to source
control; the local Compose password was process-local only.

## Unresolved assumptions and limitations

- Historical odds availability at any historical prediction cutoff remains
  unverified. The source timestamp is not treated as a publication timestamp.
- Market status `-3`, inactive outcomes, corrections, voids, and market-specific
  settlement semantics remain unresolved. Only 1X2 probability normalization is
  implemented for baselines.
- Market 1 outcome IDs and labels were observed in this batch but are not a
  provider versioned contract; ambiguous lines are excluded.
- Historical seasons discovered but not imported remain deferred by design.
  The live discovery response returned 167 completed candidates; the run
  imported/reused three and deferred 165.
- The live batch reported invalid zero odds in markets such as correct score,
  exact goals, and other market variants. Those raw outcomes remain quarantined;
  they are not silently converted to prices.
- The evaluation has only two held-out seasons and retrospective odds, so it is
  unsuitable for model promotion or any claim of a tradable edge.

## Next phase plan

1. Review the two-season coverage/quarantine report and provider contract
   findings with the owner.
2. If approved, add further bounded completed seasons in small batches using
   the same dry-run and coverage gates; never remove the ten-second budget.
3. Resolve or document verified historical odds availability before enabling a
   timestamp-faithful odds feature track or ROI claim.
4. Expand market-specific settlement/probability adapters only after provider
   semantics are validated.
5. Keep complex ML, dashboard/authentication, live scheduling, and automated
   betting out of scope until a later explicitly authorized phase.
