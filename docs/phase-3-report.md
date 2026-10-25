# Phase 3 implementation and verification report

Date: 2026-10-25

Phase 3 adds controlled historical expansion, independently runnable live
monitoring, a transactional pre-match prediction lifecycle, forward paper
settlement, and loopback-only operational read endpoints. It does not add
public authentication, a dashboard, complex machine learning, or automated
betting.

## Delivered

- `db/migrations/004_phase3_operational.sql` adds live observations, append-only
  odds-change history, round states, prediction runs/revisions, settlements,
  forward paper metrics, scheduler leases, a persistent cross-process provider
  request budget, and job execution history.
- `db/migrations/005_phase3_observation_history.sql` through
  `007_phase3_odds_observation_history.sql` preserve repeated cached/live
  observations and add append-only odds observation provenance. A later
  post-kickoff refresh can no longer erase the fact that an earlier local
  observation was pre-kickoff.
- Frozen prediction runs/revisions have database immutability triggers. A
  complete round requires eight same-round fixtures and is inserted in one
  transaction; incomplete and missed-cutoff attempts are recorded as explicit
  statuses without partial prediction rows.
- `services/live` implements season discovery, current fixture/result
  reconciliation, upcoming/future odds collection, cache-aware raw retention,
  odds change detection, adaptive polling, circuit breaking, leases, restart
  recovery, and per-fixture pre-kickoff verification.
- `services/prediction` uses the Phase 2 statistical baselines only. It stores
  model version, input fingerprint, cutoff, feature provenance, odds
  observation time, provisional/frozen revisions, and secondary Poisson
  distributions where supported.
- `services/settlement` records forward-observed settlement metrics and
  correction versions. It excludes post-kickoff odds from hypothetical flat
  stake arithmetic and keeps historical retrospective research separate.
- `cmd/ops-api` exposes read-only internal endpoints for current state,
  fixtures, predictions, history, metrics, and feed/job health. It binds to
  loopback by default and requires an internal token for non-loopback use.
- No credentials were added to source control. No provider request is made by
  offline verification commands.

## Controlled historical expansion

The importer cap is now 30 seasons per invocation; the default remains one.
The operator command is explicit and resumable:

```powershell
$env:DATABASE_URL = 'postgres://vfl:<local-password>@localhost:<port>/vfl?sslmode=disable'
go run ./cmd/historical-ingest -allow-network -max-seasons 20 -max-retries 1 -report tmp/phase3-expansion-20.json
```

The run reuses complete seasons, persists raw payloads/checkpoints/quarantine,
and uses the shared ten-second minimum spacing. The explicitly authorized
bounded run completed with the following coverage:

| Measure | Result |
|---|---:|
| Provider seasons discovered | 168 |
| Completed seasons available | 167 |
| Newly selected seasons | 20 |
| Reused complete seasons | 3 |
| Deferred completed seasons | 147 |
| Selected fixture rows | 4,800 |
| Selected odds rounds attempted | 600 |
| Selected odds coverage | 30 rounds per season, eight joined matches per round |
| Accepted outcome rows | 671,812 |
| Quarantined invalid outcomes | 652 |
| Fixture validation errors | 0 |
| Fixture/odds join failures | 0 |

All 20 selected seasons have 240 fixtures, 30 fixture rounds, and coverage
records for all 30 odds rounds. The importer report calls only 183 rounds
error-free because 417 rounds contained at least one invalid provider outcome;
those rows were quarantined rather than treated as missing or silently
discarded. The batch was started before the persistent cross-process request
gate was added, so this run used the existing in-process ten-second budget;
the persistent gate is used by the current monitor/ingestion code and is
covered by integration tests. It is not a catalog-wide import.

The source report is retained at
`tmp/phase3-expansion-20.json` for audit and reprocessing.

## Expanded historical evaluation

The expanded offline evaluation was persisted in
`tmp/phase3-expanded-evaluation.json`:

- 23 structurally complete seasons (the three reused seasons plus the 20
  selected seasons), 5,520 final matches, and 5,520 feature vectors.
- 22 seasons/5,280 matches were evaluable in walk-forward mode because the
  earliest season has no eligible prior-season training history.
- Historical odds were explicitly evaluated on the
  `retrospective_unverified_availability` track. They were not represented as
  verified prices at any historical cutoff.
- Block-bootstrap intervals use 400 season blocks/replicates; they are
  uncertainty intervals for this retrospective sample, not evidence of a
  tradable edge.

| Model | Log loss | 95% block interval | Multiclass Brier | Accuracy | ECE |
|---|---:|---:|---:|---:|---:|
| Overall frequency | 1.062323 | 1.054079–1.070880 | 0.642116 | 0.454735 | 0.005488 |
| Team strength | 1.079839 | 1.064796–1.095465 | 0.638909 | 0.536174 | 0.103202 |
| Poisson | 0.984872 | 0.970862–1.001983 | 0.586359 | 0.538068 | 0.018849 |
| Bookmaker implied | 0.979353 | 0.966996–0.993653 | 0.583145 | 0.537500 | 0.018624 |
| Statistical/bookmaker combination | 0.979568 | 0.966739–0.994979 | 0.583132 | 0.538068 | 0.015788 |

Bookmaker-implied probabilities had the best log loss and nearly the best
Brier score. The combination had the smallest Brier score by a negligible
margin, while its log loss was slightly worse than bookmaker-only. The
intervals overlap, so this run does not establish a statistically significant
advantage. Research-only flat-stake analysis produced 677 retrospective
selections, 33.68% hit rate, -5.37% hypothetical ROI, and 55.15 maximum
drawdown; those results are not a claim about executable historical prices.

## Live contract validation

One bounded read-only live cycle completed against the documented feed:

| Observation | Finding |
|---|---|
| Live current season | `3149484` |
| Fixtures | 240 across 30 rounds; the observed upcoming rounds had eight fixtures each |
| Odds rounds | Two rounds were fetched in the cycle, eight odds match records each |
| Fixture/odds joins | 16/16 observed matches joined; no team-ID join failures |
| Live odds rows | 3,348 normalized rows; 3,347 valid and one invalid retained/quarantined |
| Market catalog | 23 provider market IDs observed; market `1` supplied the 1X2 line; 35 distinct specifier strings were retained; valid decimal odds ranged from 1.00 to 100.00 |
| Market/outcome indicators | statuses ranged from `-3` to `1`, active values from `0` to `1`, provider results from `-1` to `1`; market `1` used team labels plus `draw` |
| Cache behavior | One conditional/cache-reused fixture observation was recorded; response metadata remains stored |
| Odds changes | Initial and meaningful change history was persisted; identical cached payloads are deduplicated |

The live and historical feed bases exposed different current-season signals:
the historical discovery feed reported `3149454`, while the live feed reported
`3149484`. Both raw observations are retained and the operational API prefers
the latest live fixture observation. This remains an unresolved provider
contract assumption, not an inferred reconciliation.

The provider `timestamp` remains a source timestamp. Only a local
`fetched_at` preceding the fixture kickoff can mark a live odds row as
point-in-time eligible. Post-kickoff rows remain auditable and are never
treated as executable pre-match prices.

## Prediction and forward-paper verification

An offline lifecycle check generated a complete eight-fixture provisional run
for season `3149484`, round 14, and a separate frozen run before its stored
cutoff. The database tests confirmed that changing a frozen probability fails,
leases are acquired/released, repeated generation is idempotent, interrupted
ingestion resumes, and a corrected final result creates a new settlement
version rather than overwriting the prior one. The settlement test used
synthetic forward observations; no production forward-paper performance claim
is made until final results and genuine pre-kickoff odds are observed and
reconciled. In the recorded lifecycle check, the provisional run was created
at `2026-10-25T02:35:37Z`, its cutoff was
`2026-10-25T02:38:04Z`, the earliest kickoff was
`2026-10-25T02:38:34Z`, and the official frozen run was stored at
`2026-10-25T02:36:38Z`. This is a verification-database timing result, not a
claim that a production prediction was published to users.

Phase 2's retrospective model comparison remains the historical baseline
reference. Historical odds returned after settlement remain explicitly
unverified for point-in-time use; forward-paper results will be reported only
from snapshots observed by this application before the cutoff.

## Verification commands

```powershell
gofmt -l services
go vet ./...
go test -p 1 -count=1 ./...
go run ./cmd/phase3-verify
./scripts/verify-phase3.ps1
```

With a dedicated local PostgreSQL database:

```powershell
$env:PHASE1_DATABASE_URL = $env:DATABASE_URL
$env:PHASE2_DATABASE_URL = $env:DATABASE_URL
$env:PHASE3_DATABASE_URL = $env:DATABASE_URL
go test -p 1 -count=1 ./services/ingest ./services/research ./services/prediction ./services/settlement -run 'Test(PostgresIntegrationIdempotencyAndInterruptedRecovery|Phase2DatabaseSchemaAndResearchStorage|Phase3DatabaseSchemaAndFrozenGuard|Phase3PredictionGenerationIsIdempotent|Phase3ForwardSettlementIntegration)' -v
```

The Phase 3 verification script includes this integration-test selection when
`PHASE3_DATABASE_URL` is set.

Final verification completed successfully on 2026-10-25: `go vet ./...`, the
full Go suite with all local PostgreSQL integrations enabled, the Phase 3
verification script, Python compilation, five Python model tests, and Docker
Compose configuration validation all passed. The Phase 3 verifier reported
`network_requests: 0`. The local database password used during verification
was a command-line development placeholder and was not written to source
control.

## Unresolved assumptions and limitations

See [api-assumptions.md](api-assumptions.md), especially API-02–04,
API-10–18, and API-23–30. The important remaining limitations are provider
refresh/quota semantics, market status and correction semantics, exact odds
publication/availability time, cross-feed season transitions, and settlement
rules beyond final-score 1X2 research arithmetic. The historical expansion is
rate-limited enough that large batches can take hours; no unrestricted import
is started by default.

## Next phase plan

1. Review the 20-season coverage/quarantine report and recurring zero-odds
   market behavior before selecting another bounded batch.
2. Continue timestamped live observations across several refreshes to measure
   feed freshness, odds changes, and cross-feed season transitions without
   relaxing the request budget.
3. Reconcile the frozen live round and report forward-paper metrics by season
   and round only after genuine pre-kickoff odds are available.
4. Keep public authentication/dashboard work and any complex model promotion
   behind the unresolved source-contract and forward-evaluation gates.
