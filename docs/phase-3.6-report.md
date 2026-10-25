# Phase 3.6 implementation and verification report

Date: 2026-10-25  
Scope: sustained live validation and operational readiness only

## Outcome

Phase 3.6 operational work is implemented and verified. The system now has
explicit lease inspection, PostgreSQL-enforced expired-only lease recovery,
age-bounded orphaned-job reconciliation, round-state transition protection,
provider-backed live evidence reporting, and prediction-worker execution
history.

A sustained observation covering three complete VFL seasons was not possible
within this execution window. No polling interval was shortened and no
retrospective data was counted as forward evidence. A bounded live cycle did
complete successfully, and the operator command for a longer supervisor-run
validation is documented below.

## Lease ownership and recovery

The previous blocker was inspected before any mutation. The original
`live-monitor` lease was owned by `live-monitor-13772`, had expired at
10:05:41 local time, and was inspected at 10:36:09 local time. No corresponding
worker process was present. The lease was reclaimed by the database predicate
`lease_until <= now()`; it was not force-deleted while valid.

A subsequent bounded attempt acquired a new lease, persisted one discovery
response, and was interrupted by the execution environment. Its lease expired
at 10:40:47 local time. After another process check, it was reclaimed safely.
Three old `running` live-monitor job records were marked `cancelled` by the
age-bounded orphan recovery path. The final report has no active live-monitor
lease and no active live-monitor job record.

The new PostgreSQL integration test proves:

- two owners cannot acquire the same unexpired lease;
- a valid lease is not reclaimed;
- an expired lease is reclaimed atomically;
- running job records are cancelled during expired-lease recovery;
- an owner can acquire the key after recovery; and
- old jobs are reconciled only when no valid lease exists.

## Actual live observations

The completed Phase 3.6 cycle ran with retries disabled and the existing
shared provider request gate configured to a ten-second minimum spacing. It
observed:

| Observation | Result |
|---|---:|
| Live season discovered | `3149569` |
| Fixtures returned | 240 |
| Upcoming rounds | 5 and 6 |
| Fixture count per upcoming round | 8 and 8 |
| Round 5 normalized odds | 1,129 accepted, 1 quarantined |
| Round 6 normalized odds | 1,119 accepted, 1 quarantined |
| Odds changes recorded in cycle | 2,248 |
| Provider HTTP errors in persisted live observations | 0 |
| Cache-reused live observations in final report | 1 |

The final operational report contains 14 provider live observations across
discovery, fixtures, and odds, covering three observed season identifiers over
the retained local series. Across the retained live odds evidence there are
5,598 live observation rows: 2,236 locally point-in-time verified and 3,362
unverified/post-cutoff rows. The provider timestamp remains separate from the
local observation timestamp.

The latest live discovery response identifies `3149569` as current and lists
`3149540` and `3149484` immediately before it. The historical season-list feed
still reports `3149454` as current. This cross-feed discrepancy remains
unresolved; neither source is silently substituted for the other.

The cycle did not observe a 429 or `Retry-After`, so it does not establish
provider quota headroom or long-term refresh characteristics. The recorded
response and observation counts are not claimed to be wire-level request
counts when cache reuse or retries are involved.

## Forward prediction and cutoff evidence

Two provider-backed complete frozen rounds are present in the local database:

| Season/round | Frozen at (UTC) | Cutoff (UTC) | Earliest kickoff (UTC) | Fixtures | Result |
|---|---|---|---|---:|---|
| `3149484` / 14 | 02:36:38 | 02:38:04 | 02:38:34 | 8 | complete and pre-cutoff |
| `3149569` / 6 | 07:44:27 | 07:44:44 | 07:45:14 | 8 | complete and pre-cutoff |

All eight revisions in each round were frozen before kickoff and carried
locally verified pre-cutoff odds metadata. The final report preserves the
prediction model version (`phase2.v1`), prediction time, cutoff, freeze time,
and odds observation time for each revision.

Round 5 was observed after its 07:41:04 UTC cutoff and was marked
`missed_cutoff`. A deliberately late manual prediction invocation for round 6
finished at 07:44:50 UTC, after its 07:44:44 UTC cutoff; the engine persisted a
`missed_cutoff` run with zero generated fixtures and no revisions. An
independent frozen run already present in the database had been frozen at
07:44:27 UTC. The late invocation is not credited with creating that frozen
run, and the immutable frozen run was preserved.

The state-downgrade defect exposed by this sequence was fixed: live or late
prediction updates can no longer change a `frozen` round to `missed_cutoff`, or
a `settled` round to an earlier state. One guarded repair restored the current
round state from `missed_cutoff` to `frozen` because a complete frozen run was
provably before kickoff.

There are no provider-backed settlements yet. The 25 recorded
`forward_observed` settlement rows in the development database are synthetic
integration evidence with no eligible odds and are excluded from this report's
genuine forward metrics. Consequently, genuine forward log loss, Brier score,
accuracy, ROI, drawdown, and bookmaker comparison are not reported as
performance claims. No betting advantage is claimed.

## Verification and artifacts

Added or changed:

- `services/ingest/operational.go`: lease inspection, safe recovery, orphan
  reconciliation, and frozen/settled round-state protection.
- `services/ingest/repository.go`: PostgreSQL advisory migration lock so
  concurrent worker/test startup cannot deadlock during the large idempotent
  migration backfill.
- `services/prediction/engine.go`: matching round-state protection.
- `cmd/phase36-lease`: read-only lease inspection plus explicit recovery flags.
- `cmd/phase36-report`: JSON operational, round, odds-provenance, settlement,
  and job-health report generator.
- `cmd/phase36-verify`: offline and optional database verification.
- `services/ingest/lease_integration_test.go` and round-state integration
  tests: ownership, recovery, orphan handling, and downgrade prevention.
- `cmd/prediction-worker`: persistent job execution records for future worker
  runs.
- `scripts/verify-phase36.ps1`: offline-by-default Phase 3.6 verification;
  `-RunLiveCycle` is an explicit network opt-in.
- `tmp/phase36-forward-report.json`: generated local evidence report; it is
  ignored by Git.

The targeted PostgreSQL lease and round-state integration tests passed. The
final `scripts/verify-phase36.ps1 -DatabaseUrl ...` run exited 0 after rerunning
the Phase 0–3.5 suites, all Go packages, PostgreSQL integration tests, Python
tests, Compose validation, and Phase 3.6 checks. The verifier reported zero
provider requests because no live flag was supplied.

## Verification commands

Offline checks:

```powershell
./scripts/verify-phase36.ps1
```

With the dedicated local PostgreSQL database:

```powershell
$env:PHASE36_DATABASE_URL = $env:DATABASE_URL
./scripts/verify-phase36.ps1 -DatabaseUrl $env:PHASE36_DATABASE_URL
```

Generate only the local report:

```powershell
go run ./cmd/phase36-report -database-url $env:PHASE36_DATABASE_URL -report tmp/phase36-forward-report.json
```

The report JSON is operational evidence, not a public performance or trading
claim. The development database also contains rows created by the prior phase
integration suites; `provider_backed=false` marks those rows so they are not
counted as live evidence.

## Operational startup and shutdown

1. Start PostgreSQL with `docker compose -f infra/docker/docker-compose.yml up -d postgres`.
2. Inspect the lease with `go run ./cmd/phase36-lease -database-url $env:PHASE36_DATABASE_URL`.
3. If it is expired and the owner process is confirmed absent, use
   `-reclaim-expired`; if the lease row is already absent, use
   `-reconcile-orphaned-jobs`.
4. Start the live monitor under a service supervisor:

   ```powershell
   $env:BET9JA_MAX_RETRIES = '0'
   go run ./cmd/live-monitor -database-url $env:PHASE36_DATABASE_URL -allow-network -request-spacing-seconds 10
   ```

5. Run the prediction worker separately when authorized. It uses the existing
   prediction cutoff and freeze guards; it does not place bets:

   ```powershell
   go run ./cmd/prediction-worker -database-url $env:PHASE36_DATABASE_URL -model poisson -interval 60s
   ```

   After final results are available, run `go run ./cmd/forward-settle
   -database-url $env:PHASE36_DATABASE_URL` as a separate operator action.
6. Stop workers with SIGINT/SIGTERM and inspect the report after each observed
   round. Do not run duplicate owners or shorten the adaptive polling plan.

The operational API remains loopback-only/internal-only as implemented in
Phase 3. No dashboard, authentication, additional model, or automated betting
was added.

## Remaining limitations and next phase

- Three complete VFL seasons of sustained observation were not completed in
  this execution window; the safe long-running command above is required for
  that evidence.
- The two season feeds disagree on their current-season identifier.
- Provider market/status semantics and exact price publication times remain
  unresolved.
- No genuine provider-backed prediction settlement exists yet.
- The observed current round included a cutoff miss, so scheduler supervision
  and prediction-worker execution history should be watched through a longer
  run.

The next safe step is to run the documented supervisor-managed validation for
at least three complete seasons, reconcile the season-feed transition, settle
only genuinely forward frozen rounds, and review operational failure rates
before implementing any dashboard or authentication work.
