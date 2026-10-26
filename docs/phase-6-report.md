# Phase 6 implementation report

## Status

Phase 6 is implemented as the direct production prediction strategy. A
successful `main` deployment registers and activates the exact deployed API
image without running chronological replay or a pre-cutoff shadow round. The
activation is explicitly recorded as `direct_no_replay`; no claim of historical
model superiority is made by the deployment. No historical prediction, frozen revision,
settlement, model artifact, or forward-testing record is deleted or rewritten.

## Model design

The candidate is one probabilistic model, not an average of two independent
predictors.

For each team, the deterministic filter maintains:

- an opponent-adjusted Elo-style rating;
- rating variance;
- attack and defense residuals around the rating prior;
- diagonal Laplace-style attack/defense uncertainty;
- all-time and current-season match counts.

Before a completed result is processed, the model computes expected goals from

```text
eta_home = league_intercept + home_advantage
          + rating_to_goal_scale * (home_rating - away_rating)
          + home_attack_residual - away_defense_residual

eta_away = league_intercept
          - rating_to_goal_scale * (home_rating - away_rating)
          + away_attack_residual - home_defense_residual

lambda_home = exp(clamp(eta_home))
lambda_away = exp(clamp(eta_away))
```

The observed goals then update the attack/defense residuals using a
regularized penalized-Poisson step. The same result updates the opponent-
adjusted ratings with a capped `log(1 + goal_difference)` margin adjustment.
The rating term is therefore a prior/covariate in the goal likelihood; its
probability output is never averaged with a second model output.

At a season boundary, ratings and residuals carry over at `0.20` by default
and their variances are expanded toward the initial prior variance. A new
season is explicitly prepared even if it has no completed result yet, which
supports round-one predictions without inventing form history.

The default filter is deterministic and uses no MCMC. Current-season results
are the most recent state updates; historical seasons only influence the state
through the configured carryover and the replay training history.

## Unified markets

The model creates one independent-Poisson joint matrix
`P(home_goals=i, away_goals=j)`. The matrix uses an adaptive grid when the
candidate requests the default score limit and records the omitted tail mass.
All displayed markets are sums over this same normalized matrix:

- 1X2: `i > j`, `i = j`, `i < j`;
- BTTS Yes: `i >= 1 && j >= 1`, with No as its complement;
- Over 2.5: `i + j >= 3`, with Under as its complement;
- correct score: the individual matrix cell.

The implementation validates normalization and market complements in unit and
deterministic verification tests. Expected goals and dynamic team diagnostics
are stored with candidate prediction payloads for audit and dashboard display.

## Lifecycle and storage

Migration `012_phase6_dynamic_bayesian.sql` adds:

- immutable dynamic model checkpoints;
- isolated `prediction_shadow_runs` and `prediction_shadow_revisions`;
- a singleton `production_model_selection` audit record;
- registry support for the `dynamic_bayesian` algorithm.

The ordinary prediction worker accepts `PREDICTION_MODEL`. The release
workflow persists `dynamic_bayesian_team_strength` in the protected production
environment after migrations and restarts only `prediction-worker`. Direct
activation also creates an auditable active model artifact and production
selection row with replay and shadow evaluation explicitly marked `not_run`.
It is not a performance claim and does not silently fall back to the retired
strategy.

The shadow command writes only the shadow tables. When both selector flags are
omitted, it selects the next eligible future round automatically:

```powershell
go run ./cmd/phase6-shadow -database-url $env:DATABASE_URL
```

The command requires a complete eight-fixture round and a pre-cutoff run. It is
idempotent for the same model definition, round, and input fingerprint.

## Replay and verification

Run the deterministic offline gate:

```powershell
go run ./cmd/phase6-verify
.\scripts\verify-phase6.ps1
```

Run chronological research against a disposable or production-read-only data
copy:

```powershell
go run ./cmd/phase6-evaluate -database-url $env:DATABASE_URL `
  -report tmp/phase6-replay-report.json
```

The replay refits before each target round using final results strictly before
the cutoff, excludes retrospective odds by default, reports 1X2, BTTS,
Over/Under 2.5, correct-score likelihood, season/round slices, and
season-block uncertainty intervals. It does not create production prediction
runs.

## Optional research promotion gate

The optional research/promotion path can run these gates when requested:

1. normalized probabilities and scoreline invariants;
2. no-future-leakage replay tests;
3. all eight fixtures in shadow rounds;
4. latency and memory on the VPS-sized environment;
5. comparison against bookmaker-first, Poisson, and verified bookmaker odds;
6. early-, mid-, and late-season calibration;
7. genuine forward shadow observations.

After the replay and shadow commands pass, the optional
`scripts/auto-promote-phase6-production.sh` wrapper can register, approve, and
select the exact deployed API image atomically. The underlying command is:

```powershell
go run ./cmd/phase6-promote -database-url $env:DATABASE_URL `
  -artifact-id <approved-model-artifact-uuid> `
  -selected-by <operator> `
  -reason "Phase 6 acceptance gates reviewed"
```

This retires the previous selected artifact in the registry but does not
rewrite old predictions. Image rollback and model-selection rollback remain
separate operations from database migration rollback.

For the deployed Compose environment, the API image now contains the Phase 6
activation, evaluation, shadow, and promotion binaries. The host-side promotion script
requires both a saved replay report and a saved shadow report, requires an
explicit `CONFIRM_PROMOTION=YES`, selects only an already-approved artifact,
updates only `PREDICTION_MODEL`, and restarts only `prediction-worker`:

```bash
APP_DIR=/opt/bet9ja-vfl \
GHCR_IMAGE_PREFIX=ghcr.io/<owner>/<repository> \
ARTIFACT_ID=<approved-model-artifact-uuid> \
PHASE6_EVALUATION_REPORT=/opt/bet9ja-vfl/phase6/phase6-replay-report.json \
PHASE6_SHADOW_REPORT=/opt/bet9ja-vfl/phase6/phase6-shadow-report.json \
CONFIRM_PROMOTION=YES \
bash /opt/bet9ja-vfl/releases/<phase6-release-sha>/promote-phase6-production.sh
```

The release workflow uploads the direct activation script and invokes it after
deployment. The script keeps a mode-protected backup of the environment file,
uses a separate model-promotion lock, verifies the immutable API digest, and
never removes or recreates the PostgreSQL volume. If the worker verification
fails, inspect the backup and current worker state before taking any corrective
action; do not reverse database migrations automatically.

The optional research wrapper runs replay, shadow, registration, and approval
commands through the dedicated `phase6-research` Compose service. That one-shot
service is capped at 1536 MB and 1.25 CPU so it cannot inherit the ordinary
migration runner's 512 MB limit or interfere with long-running workers. The
normal deployment does not start this service.

The candidate registry path remains available for diagnostics or a separately
audited operator run. The normal `main` workflow instead performs direct
activation:

```bash
APP_DIR=/opt/bet9ja-vfl \
GHCR_IMAGE_PREFIX=ghcr.io/<owner>/<repository> \
CONFIRM_ACTIVATION=YES \
bash /opt/bet9ja-vfl/releases/<release-sha>/activate-phase6-production.sh
```

The following commands remain available when a replay and shadow evaluation is
desired separately:

```bash
mkdir -p /opt/bet9ja-vfl/phase6
docker compose --env-file /opt/bet9ja-vfl/.env.production \
  -f /opt/bet9ja-vfl/docker-compose.production.yml \
  run --rm --no-deps -v /opt/bet9ja-vfl/phase6:/var/phase6 \
  --entrypoint /app/phase6-evaluate phase6-research \
  -report /var/phase6/phase6-replay-report.json \
  -bootstrap-replicates 100

# Run this before a real round cutoff; repeat for the controlled shadow sample.
docker compose --env-file /opt/bet9ja-vfl/.env.production \
  -f /opt/bet9ja-vfl/docker-compose.production.yml \
  run --rm --no-deps --entrypoint /app/phase6-shadow phase6-research \
  -season-id <season-id> -round <round> \
  > /opt/bet9ja-vfl/phase6/phase6-shadow-report.json

docker compose --env-file /opt/bet9ja-vfl/.env.production \
  -f /opt/bet9ja-vfl/docker-compose.production.yml \
  run --rm --no-deps -v /opt/bet9ja-vfl/phase6:/var/phase6:ro \
  --entrypoint /app/phase6-register phase6-research \
  -report /var/phase6/phase6-replay-report.json \
  -artifact-path ghcr.io/<owner>/<repository>/vfl-api@sha256:<api-image-digest> \
  -artifact-sha256 <64-hex-api-image-digest>

docker compose --env-file /opt/bet9ja-vfl/.env.production \
  -f /opt/bet9ja-vfl/docker-compose.production.yml \
  run --rm --no-deps --entrypoint /app/phase6-approve phase6-research \
  -artifact-id <candidate-artifact-uuid> \
  -approved-by <operator> -confirm YES \
  -reason "Chronological replay and shadow gates reviewed"
```

The `phase6-register` command records the replay as a completed experiment but
leaves the artifact at `candidate`. `phase6-approve` only accepts the exact
Phase 6 model key/version and records approval metadata. Only the optional
promotion path uses those gates; the normal deployment uses the explicit
direct-activation path described above.

## Verification result and limitations

The offline Phase 6 checks pass in this workspace: scoreline normalization,
market consistency, deterministic state updates, season preparation, early-
season priors, chronological replay, and future-result leakage prevention.

Live historical coverage, model comparison numbers, production shadow
performance, and resource measurements require access to the deployed
PostgreSQL dataset and must be generated separately with `phase6-evaluate` and
`phase6-shadow`. They are not fabricated here. The production model is active
by explicit policy, not because replay has demonstrated superiority.
