# Phase 1 implementation and verification report

Date: 2026-10-25

Phase 1 is limited to live contract validation, PostgreSQL persistence, conservative historical ingestion scaffolding, and verification. Prediction algorithms, model training, dashboard features, authentication, and live prediction scheduling are not included.

## Live contract validation

An authorized read-only probe used the documented Bet9ja feed URLs without credentials:

- Live season discovery returned 168 records and identified current season `3149454`. The probe selected completed season `3149425`, not the current season.
- `stats_season_fixtures2/3149425` returned 240 distinct fixtures across 30 rounds. Every round contained eight fixtures, 16 team UIDs were present, and all 240 result pairs were non-null. The provider still returned `status: null` and `inlivescore: true` for the completed response.
- `stats_season_odds2/vf:season:3149425/1` returned eight odds match records. All eight joined fixture IDs after normalizing `vf:match:<numeric>` to the numeric fixture ID, and the 16 fixture/odds team UID sets matched.
- The round contained 347 market objects, 1,118 outcome objects, market IDs including `1`, `18`, `47`, `68`, and `199`, null specifiers, market status `-3`, outcome `active=0`, settled outcome `result` values `0` and `1`, and one non-positive `0.00` price.
- A current-season fixture response for `3149454` returned 128 final score pairs and 112 partial/null score pairs. The 112 unsettled records used `periods: []`; scored records used a periods object.
- The odds feed was available after the completed round settled. Its millisecond source timestamp was stored separately from the HTTP observation time. This confirms retrospective availability only; it does not prove opening/closing price semantics or pre-cutoff availability.
- Fixture responses exposed `Cache-Control`/`Last-Modified`; no ETag was observed. An unsupported fixture path returned HTTP 200 with an error-shaped `data` object, so the parser must validate the body contract rather than trust HTTP status.

See [api-assumptions.md](api-assumptions.md) for the assumption register update. No live response body was copied into tracked fixtures, and no credentials were written.

## Implemented

- `db/migrations/001_phase1_initial.sql` and `002_phase1_raw_payload_text.sql` create normalized seasons, teams, fixtures/results, market definitions, odds snapshots, raw payload/provenance storage (including malformed-body retention), jobs, checkpoints, failures, validation quarantine, and coverage views.
- `services/ingest` provides a typed HTTP client with a shared request budget, default 10-second spacing, cache validators, `Retry-After` support, bounded exponential retry, raw payload persistence, idempotent upserts, round checkpoints, join validation, and coverage reporting.
- `cmd/historical-ingest` is bounded to one completed season by default and refuses a batch larger than ten seasons. Network access requires `-allow-network` or the explicit connectivity environment setting. It does not run an unrestricted backfill.
- `services/provider/seasons.go` adds typed season discovery parsing while preserving the Phase 0 fixture and odds parser behavior.
- Golden parser, join-validation, HTTP retry/cache, bounded-selection, idempotency, and interrupted-checkpoint recovery tests were added. PostgreSQL integration tests run when `PHASE1_DATABASE_URL` points at a dedicated database; otherwise they skip safely.
- `scripts/verify-phase1.ps1` runs formatting, vetting, Go/Python tests, Compose validation, and the optional database integration suite.

## Coverage and execution status

The offline golden tests pass. The live probe completed its controlled validation. The bounded single-season import for `3149425` completed against the dedicated local PostgreSQL container:

- 240/240 fixtures persisted across 30/30 rounds; every round has eight fixtures.
- 8/8 odds match records joined in every round; no odds-to-fixture join failures were recorded.
- 33,612 outcome snapshots were retained, of which 33,582 have usable positive decimal odds.
- 30 `0.00` outcomes were retained as raw/normalized invalid rows and quarantined; no malformed value was silently dropped.
- 11 odds checkpoints completed cleanly and 19 completed with explicit validation errors. A rerun uses the checkpoints rather than repeating completed odds rounds.
- All historical odds rows are marked `observation_kind=historical_returned_after_settlement` and `point_in_time_verified=false`.
- The live discovery report saw 168 seasons, 167 completed candidates, selected one season, and deferred the other 166 completed candidates by design. The CLI report includes this bounded-selection state plus per-round fixture/odds/market coverage and quarantine/join counts.

The repository has no committed credentials or provider secrets. No multi-season backfill was launched.

To run the conservative end-to-end import after starting a local database:

```powershell
$env:POSTGRES_PASSWORD = 'use-a-local-untracked-password'
docker compose -f infra/docker/docker-compose.yml up -d postgres
$env:DATABASE_URL = 'postgres://vfl:use-a-local-untracked-password@localhost:5432/vfl?sslmode=disable'
go run ./cmd/historical-ingest -allow-network -season-id 3149425 -max-seasons 1 -report tmp/phase1-3149425.json
```

The command is an explicit operator action, not an unrestricted startup behavior. The Phase 1 verification run used exactly this bounded single-season scope and the configured ten-second request spacing.

## Known limitations and unresolved assumptions

- The provider odds timestamp has no verified publication or availability semantics. All historical imports are retrospective and point-in-time unverified.
- Provider market status `-3`, inactive outcomes, invalid odds, correction behavior, and market-specific settlement rules remain unresolved.
- The live season list differed from the supplied sample by one record; pagination and retention are not established.
- The live probe covered one completed season and one odds round. It did not validate every historical round or every endpoint family.
- Provider error envelopes can use HTTP 200, so body validation and raw retention are required.
- The PostgreSQL integration test requires a dedicated local PostgreSQL database. It is not a substitute for a production migration review.
- Network ingestion rejects a spacing value below ten seconds in the Phase 1 CLI; unit tests may inject zero spacing because they use local test transports.

## Next phase plan

1. Run the bounded single-season import against a dedicated local PostgreSQL database and review its coverage/quarantine report.
2. Resolve remaining provider semantics with additional authorized probes, especially odds timing, market status/settlement, and retention/pagination.
3. Expand only to a small operator-selected completed-season batch after the single-season report is clean or all discrepancies are explicitly accepted.
4. Keep prediction/model work out of the ingestion boundary until point-in-time provenance and fixture/odds coverage policies are signed off.
