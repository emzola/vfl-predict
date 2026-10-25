# Phase 3.5 implementation and verification report

Date: 2026-10-25

Phase 3.5 adds a deterministic LightGBM research path, chronological model
comparison, artifact registry integration, and a runnable live-validation
procedure. It does not add a dashboard, public authentication, automated
betting, or automatic model promotion.

## Executive result

LightGBM did not outperform bookmaker-implied probabilities on the untouched
final seasons. The existing bookmaker baseline remains the only fallback and
no Phase 3.5 artifact was promoted. Adding bookmaker probabilities improved
LightGBM relative to the no-odds LightGBM variant, but it still remained worse
than the bookmaker baseline. The residual correction improved the no-odds
direct model, but not the bookmaker baseline. The learned ensemble selected a
zero LightGBM weight and therefore reduced exactly to the bookmaker baseline.

Historical odds in this experiment are explicitly
`retrospective_unverified_availability_explicit_opt_in`. They were returned by
the provider after settlement and are useful for retrospective research, but
they are not evidence that the prices were available at the simulated
decision cutoff.

## Delivered

- `services/research/phase35_export.go` and `cmd/phase35-export` export the
  canonical Go point-in-time feature vectors, stable team identifiers,
  baseline probabilities, and explicitly labeled bookmaker provenance.
- `services/model/vfl_lgbm/` contains the Python native-LightGBM training,
  inference, calibration, residual correction, ensemble, metrics,
  chronological-fold, artifact, and report implementation.
- `db/migrations/008_phase35_model_registry.sql` adds immutable model artifact
  records and reproducible experiment records.
- `services/research/model_registry.go` and `cmd/phase35-register` provide
  checksum-based idempotent registration and explicit candidate/approved/active/
  retired transitions. Phase 3.5 registration never promotes automatically.
- `cmd/phase35-verify` and `scripts/verify-phase35.ps1` verify reports,
  artifact checksums, registry state, prior phase suites, Python tests, and
  Compose configuration.
- `scripts/validate-phase35-live.ps1` is an explicit, offline-by-default
  procedure for one authorized live read-only cycle.

No credentials were added to source control and no commit was created.

## Dataset and chronological protocol

The export used the existing controlled local database, with no new bulk
historical import. It contained 23 structurally complete seasons and 5,520
matches, all with a usable retrospective 1X2 line. The Go loader excluded 55
incomplete season records from the exported complete-season research set. The
provider catalog contains additional seasons, but no unrestricted expansion was
started.

The newest three seasons were held out as an untouched final test set:

- Final seasons: `3149394`, `3149425`, `3149454`
- Final test fixtures: 720
- Development seasons: 20
- Final test features used the same eligible fixture set for every model.

Hyperparameter search used four chronological validation folds. Each fold
trained on earlier seasons and validated on the next season. Calibration,
residual strength, and ensemble weight were fit from development validation
rows only. The final test block was not used for tuning.

Selected LightGBM configurations were regularized and early-stopping iteration
counts were selected from validation folds:

| Variant | Leaves | Depth | Learning rate | Min leaf samples | L1 | L2 | Selected iterations |
|---|---:|---:|---:|---:|---:|---:|---:|
| Without bookmaker | 7 | 3 | 0.04 | 40 | 0.2 | 5.0 | 56 |
| With bookmaker | 31 | 7 | 0.03 | 50 | 0.5 | 8.0 | 63 |

The run used seed `20261025`, one LightGBM thread, deterministic training,
fixed feature ordering, and a bounded four-candidate search.

## Final-test model comparison

Metrics below are on the 720 final-test fixtures. Lower is better for log loss,
multiclass Brier score, and ECE; higher is better for accuracy.

| Model | Log loss | Brier | Accuracy | ECE |
|---|---:|---:|---:|---:|
| Bookmaker implied | 0.989690 | 0.589963 | 0.533333 | 0.022146 |
| Poisson | 0.995634 | 0.593151 | 0.529167 | 0.028362 |
| Statistical/bookmaker combination | 0.990989 | 0.590508 | 0.520833 | 0.020333 |
| LightGBM without bookmaker | 0.996370 | 0.593960 | 0.520833 | 0.018173 |
| LightGBM with bookmaker | 0.992593 | 0.591461 | 0.533333 | 0.034808 |
| Bookmaker residual correction | 0.990703 | 0.590385 | 0.529167 | 0.025054 |
| LightGBM/bookmaker ensemble | 0.989690 | 0.589963 | 0.533333 | 0.022146 |

The bookmaker baseline had the best final-test log loss and Brier score. The
ensemble weight selected on development validation was `0.0` for LightGBM,
which is why the ensemble matches bookmaker-only exactly.

### Paired season-block uncertainty intervals

The intervals below are candidate minus bookmaker on the final test block;
negative values favor the candidate. They are 400 season-block bootstrap
replicates, not a claim of statistical significance.

| Model | Log-loss difference 95% interval | Brier difference 95% interval |
|---|---:|---:|
| Poisson | 0.003525 to 0.008509 | 0.001115 to 0.004944 |
| Statistical/bookmaker combination | 0.000551 to 0.002513 | -0.000063 to 0.001377 |
| LightGBM without bookmaker | -0.002031 to 0.020434 | -0.002101 to 0.012727 |
| LightGBM with bookmaker | -0.003139 to 0.012658 | -0.003015 to 0.007756 |
| Bookmaker residual correction | -0.004276 to 0.004340 | -0.002748 to 0.002152 |
| LightGBM/bookmaker ensemble | 0.000000 to 0.000000 | approximately 0.000000 to 0.000000 |

No LightGBM candidate met the gate requiring out-of-sample improvement in both
primary metrics with paired intervals supporting improvement. All registered
artifacts remain `candidate`.

### Season variation

The direction was not stable across the three final seasons. Raw log loss by
season was:

| Season | Bookmaker | LGBM without odds | LGBM with odds | Residual correction |
|---|---:|---:|---:|---:|
| `3149394` | 0.9862 | 1.0066 | 0.9988 | 0.9892 |
| `3149425` | 0.9648 | 0.9698 | 0.9640 | 0.9605 |
| `3149454` | 1.0181 | 1.0126 | 1.0149 | 1.0224 |

LightGBM or residual correction was favorable in individual seasons, but not
consistently and not with a reliable aggregate improvement.

## Calibration

Temperature scaling was fit only on the development out-of-fold validation
rows. It did not improve final-test log loss or Brier score for the direct
LightGBM variants or the bookmaker benchmark:

| Model | Raw log loss | Calibrated log loss | Raw Brier | Calibrated Brier |
|---|---:|---:|---:|---:|
| Bookmaker implied | 0.989690 | 0.990640 | 0.589963 | 0.590343 |
| LightGBM without bookmaker | 0.996370 | 0.998360 | 0.593960 | 0.595186 |
| LightGBM with bookmaker | 0.992593 | 0.994915 | 0.591461 | 0.592819 |
| Residual correction | 0.990703 | 0.990703 | 0.590385 | 0.590385 |
| Ensemble | 0.989690 | 0.990640 | 0.589963 | 0.590343 |

No calibrated candidate was promoted. The calibrated outputs remain available
as research artifacts with their temperatures and metrics recorded.

## Explainability diagnostics

Fold-level gain importance was persisted with feature stability summaries. In
the no-odds model, the leading diagnostic features were stable team identifiers,
head-to-head goal difference, head-to-head home-win rate, and recent team goal
statistics. In the with-odds model, bookmaker away/home/draw probabilities
were the strongest diagnostics, followed by team identifiers and historical
team statistics. This indicates that the bookmaker line carries substantial
incremental signal for LightGBM, but importance is not causal evidence and did
not translate into an aggregate final-test improvement over the bookmaker
itself.

## Live and forward validation

The Phase 3 bounded live evidence remains the only actual provider observation
series used here:

- Live feed current season: `3149484`.
- Historical feed current season: `3149454`; the discrepancy remains
  unresolved and both signals are retained rather than reconciled by guesswork.
- Two upcoming odds rounds were observed with 16/16 fixture/odds joins.
- 3,348 normalized live odds rows were retained; 3,347 were valid and one
  invalid decimal value was quarantined.
- Market `1` supplied the observed 1X2 line; 23 market IDs and 35 specifier
  strings were retained. Status, active, and result ranges were preserved but
  not assigned undocumented meanings.
- 1,116 rows were locally observed before kickoff and 2,232 were post-kickoff
  or otherwise ineligible. Provider timestamps remain separate from local
  observation times.

Phase 3.5 attempted to prepare one further bounded live cycle with zero
automatic retries and the mandatory 10-second request spacing. It made no new
provider request because the persistent `live-monitor` lease was held by
another owner. The lease was not force-released. The runnable procedure is
`scripts/validate-phase35-live.ps1 -AllowNetwork`.

There is no sustained Phase 3.5 forward prediction sample to report. Existing
Phase 3 lifecycle checks covered transactional generation, cutoff guards,
frozen immutability, restart/idempotency behavior, and synthetic settlement,
but they do not establish production forward profitability. Therefore:

- genuinely pre-cutoff frozen predictions claimed for this Phase 3.5 report: 0
- genuine forward settled fixtures claimed for this Phase 3.5 report: 0
- retrospective final-test fixtures: 720
- automated betting: not implemented

Historical and forward provenance remain separate in storage and reports.

## Registry and verification

The generated manifest contains four checksummed candidate artifacts:

- LightGBM without bookmaker odds
- LightGBM with bookmaker probabilities
- bookmaker residual correction
- LightGBM/bookmaker ensemble

The current manifest contains four candidate artifacts and 20 completed
experiments: eight chronological LightGBM search candidates, three baseline
evaluations, seven calibration evaluations, and two post-processing
evaluations. The development registry retains six candidate artifact rows for
this dataset fingerprint because two earlier checksums from the training-only
category-vocabulary correction remain immutable audit records. No artifact is
`approved` or `active`, and the bookmaker baseline remains available through
the existing Phase 2 model path.

Verification completed:

```powershell
python -m unittest discover -s services/model/tests -p 'test_*.py'
go run ./cmd/phase35-verify -report tmp/phase35-lgbm-report.json -manifest tmp/phase35-manifest.json
go run ./cmd/phase35-verify -database-url $env:PHASE35_DATABASE_URL
```

The first verifier reported no network requests; the database verifier
confirmed migration 008, six retained candidate artifact rows, 20 completed
experiments, matching current-manifest checksums, and zero non-candidate
artifacts for the evaluated dataset fingerprint.
The dedicated `scripts/verify-phase35.ps1` reruns `go vet`, all prior Go
tests/verifiers, Python compilation/tests, and Compose validation. A dedicated
PostgreSQL URL is required for its registry integration test.

## Required answers

1. LightGBM does not outperform bookmaker-implied probabilities on the unseen
   seasons in this run.
2. Adding bookmaker probabilities improves LightGBM relative to the no-odds
   LightGBM variant, but not relative to bookmaker-only.
3. Residual correction improves direct no-odds LightGBM on this test block but
   does not beat the bookmaker baseline.
4. The ensemble provides no measurable improvement; validation selected a zero
   LightGBM weight.
5. Improvements are not consistent across seasons.
6. The candidate models were not sufficiently improved by the tested
   calibration; raw outputs were retained for the fallback decision.
7. This run provides no evidence of reliable predictive value beyond the
   bookmaker baseline.
8. The live lifecycle has been validated structurally and against a bounded
   set of actual pre-kickoff odds observations, but not yet through a sustained
   series of actual frozen and settled forward predictions.

## Remaining assumptions and next-phase plan

See [api-assumptions.md](api-assumptions.md), including API-31–34. The next
safe work is to collect a sustained, timestamped live observation series,
reconcile the two season signals, and accumulate genuinely pre-cutoff frozen
predictions before considering any model approval. Any larger historical
expansion must remain an explicit bounded command and must not be used to
retroactively certify historical odds availability.
