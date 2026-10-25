# Operational scripts

`verify-phase0.ps1` runs the Phase 0 offline checks.

`verify-phase1.ps1` runs Go/Python tests, formatting/vetting, Compose parsing,
and the optional PostgreSQL integration test. It does not contact Bet9ja.

`verify-phase2.ps1` adds deterministic feature/model/backtest tests, the
offline synthetic verification command, Phase 2 schema/storage integration,
and the preserved Phase 0/1 suites.

`verify-phase3.ps1` adds the live polling, prediction lifecycle, settlement,
internal API, Phase 3 migration/lease/frozen-record integration checks, and
reruns the preserved Go research suites. It performs no provider requests.

`verify-phase35.ps1` reruns the Go suites and all prior offline verifiers,
Python contract/model tests, Compose validation, and the Phase 3.5 artifact and
registry verifier. It does not retrain or contact Bet9ja.

The bounded historical importer is the Go command
`go run ./cmd/historical-ingest`. It requires a database and an explicit
`-allow-network` flag for provider requests. It defaults to one completed
season and caps a run at thirty seasons.

Run `-dry-run` before a network import to inspect the selected seasons. The
importer reuses seasons whose 30-round fixture/odds-match coverage is complete;
it never starts an unrestricted historical backfill. Use `-max-seasons 20` (or
another explicit value up to 30) for a controlled Phase 3 expansion.

Phase 3.5 research export/evaluation is also explicit and bounded:

```powershell
go run ./cmd/phase35-export -database-url $env:DATABASE_URL -allow-retrospective-odds
python -m services.model.vfl_lgbm
go run ./cmd/phase35-register -database-url $env:DATABASE_URL
```

For a single authorized live validation cycle, use
validate-phase35-live.ps1 -AllowNetwork. It never force-releases a worker
lease; a held lease is reported as an operational blocker for investigation.

verify-phase4.ps1 reruns Phase 3.6 verification, authenticated API security
and unit checks, Compose validation, and frontend tests/typecheck/build when
the web package lockfile and installed dependencies are present. It does not
contact Bet9ja or start workers.

## Phase 4.2 storage operations

`phase42-storage-audit.ps1` runs the read-only PostgreSQL catalog audit. It
reports database size, relation/table/index/TOAST usage, logical raw-payload
bytes, WAL, row counts, PostgreSQL settings, and optionally Docker inventory:

```powershell
./scripts/phase42-storage-audit.ps1 -DatabaseUrl $env:DATABASE_URL -IncludeDocker
```

`phase42-retention-dry-run.ps1` reads migration `010_phase42_storage_policy.sql`
and reports candidates. It never deletes or archives rows:

```powershell
./scripts/phase42-retention-dry-run.ps1 -DatabaseUrl $env:DATABASE_URL
```

The Linux equivalent is `scripts/phase42-storage-check.sh`; set
`DATABASE_URL` through the service environment or a protected secret file.
The check exits nonzero before the critical free-space threshold and prints
Docker disk usage when Docker is available. The checks are suitable for a
cron/systemd timer; they do not poll Bet9ja.

Logical database recovery uses `postgres-logical-backup.sh` and the guarded
`postgres-logical-restore.sh`. Backups use PostgreSQL custom format and are
validated with `pg_restore --list`. Restore requires the explicit
`CONFIRM_RESTORE=YES` environment variable and should target a verified
recovery database.
