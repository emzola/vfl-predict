# Phase 4.2 — PostgreSQL Storage Audit and VPS Capacity Optimization

Status: implemented and verified locally on 2026-10-25. No Contabo deployment
was performed. No historical fixture, result, prediction, verified odds, or
raw provider record was deleted or rewritten. No credentials were added to the
repository.

## Executive findings

The apparent local “10 GB PostgreSQL” footprint is not the database itself.
The measured `bet9ja-vfl-dev_postgres-data` volume was 2.327 GB, while
`pg_database_size('vfl')` was 1,279,925,271 bytes (1.19 GiB / 1.28 GB decimal).
The PostgreSQL WAL directory accounted for 1,023,410,176 bytes (976 MiB) of
the volume. The largest application tables are the normalized odds value and
observation histories, not raw payloads.

The largest local Docker consumer was the build cache at 13.06 GB. The Docker
host also contains unrelated stopped images/containers from another project;
they must not be treated as Bet9ja production requirements. The Bet9ja web
workspace contained 561 MB of ignored `node_modules` and the workspace `tmp/`
directory contained 148 MB of generated files. The web build context now has
an `apps/web/.dockerignore` so local dependencies and build output are not
sent to the web image build.

The existing ingestion design already deduplicates unchanged provider bodies
by `(endpoint_family, request_key, payload_sha256)` and stores repeated local
observations separately. That is the correct provenance trade-off for this
application. Migration 010 adds policy and measurement metadata and measured
autovacuum settings, but does not introduce a risky raw-data rewrite or
lossy odds compression.

## Measurement scope and evidence

Measurements were collected from the local Docker Desktop host on 2026-10-25:

- `bet9ja-vfl-dev-postgres-1`, database `vfl`, volume
  `bet9ja-vfl-dev_postgres-data`.
- A separate Phase 4.1 disposable database was used only to apply and test
  migration 010 and the dry-run policy reader.
- Docker measurements came from `docker system df -v`, `docker volume ls`,
  read-only volume mounts, and container inspection.
- PostgreSQL measurements came from catalog/statistics queries and the new
  `cmd/phase42-storage-audit` command.
- The main development database predates migration 009, so its row counts do
  not include Phase 4 authentication tables. The Phase 4.1 disposable database
  contains those tables. This is a local fixture-state difference, not a
  production schema recommendation.

## PostgreSQL storage breakdown

### Database, volume, WAL

| Measurement | Observed value | Meaning |
|---|---:|---|
| PostgreSQL database size | 1,279,925,271 B / 1.19 GiB | Database objects, indexes, TOAST and catalog overhead |
| PostgreSQL data volume | 2,327,248,613 B / 2.327 GB | Data directory, WAL, free pages, and cluster overhead |
| WAL directory | 1,023,410,176 B / 976 MiB, 61 files | Current retained WAL files; not a permanent data-growth rate |
| Cumulative WAL since stats reset | 2,681,580,268 B | `pg_stat_wal.wal_bytes`; a counter, not allocated disk |
| `max_wal_size` | 1,024 MB | Current local setting; the observed directory is near this value |
| Replication slots | None reported | No slot-retained WAL was found |

The difference between database size and data-volume size is therefore
primarily WAL and PostgreSQL filesystem/cluster overhead, not hidden table
data. WAL growth should be alerted on, but it should not be added to a
permanent historical row-growth estimate.

### Largest relations

| Relation | Total | Main table | Indexes | TOAST | Estimated rows |
|---|---:|---:|---:|---:|---:|
| `odds_snapshots` | 595.12 MiB | 401.12 MiB | 193.85 MiB | 8 KiB | 778,793 |
| `odds_snapshot_observations` | 513.98 MiB | 379.93 MiB | 133.92 MiB | 8 KiB | 778,996 |
| `raw_payloads` | 46.27 MiB | 520 KiB | 408 KiB | 45.34 MiB | 820 |
| `research_predictions` | 31.89 MiB | 22.36 MiB | 9.49 MiB | 8 KiB | 62,976 |
| `feature_snapshots` | 12.34 MiB | 10.45 MiB | 1.86 MiB | 8 KiB | 6,492 |
| `evaluation_metrics` | 3.46 MiB | 1.30 MiB | 2.12 MiB | 8 KiB | 10,856 |
| `fixtures` | 2.71 MiB | 1.24 MiB | 1.43 MiB | 8 KiB | 6,091 |
| `odds_change_history` | 1.71 MiB | 1.15 MiB | 536 KiB | 8 KiB | 4,479 |
| `betting_analysis_selections` | 816 KiB | 472 KiB | 304 KiB | 8 KiB | 1,449 |
| `prediction_revisions` | 696 KiB | 32 KiB | 64 KiB | 576 KiB | 138 |

The remaining application tables each occupy less than 512 KiB. Indexes are
approximately 338 MiB for the two large odds tables, so index removal or
rebuilding should not be attempted without query-plan evidence. The audit
found very few dead rows in the large tables; ordinary autovacuum is currently
adequate.

### Logical payload and row counts

`raw_payloads` has 820 rows, 180,082,560 logical `payload_text` bytes, and
25,176,307 logical JSONB bytes, but only 46.27 MiB of physical relation
storage. Its 45.34 MiB TOAST footprint shows PostgreSQL compression is already
effective. The logical values overlap by design for valid JSON because the
text representation is retained for exact/audit parsing and JSONB is retained
for structured access; the current physical cost is modest relative to odds
history.

The database currently contains 270 season rows, 40 teams, 6,091 fixtures,
778,971 odds snapshots, 779,001 odds observations, 4,479 odds changes, 38
live-feed observations, 62,976 research predictions, 6,492 feature snapshots,
23 job executions, and 745 validation errors. These counts include phase
verification fixtures and test data. By observation kind, 773,343 odds
snapshots were historical-returned-after-settlement and 5,628 were live
observations; the corresponding observation-history counts were 773,343 and
5,658. Historical odds remain explicitly non-verified for point-in-time use.

## Docker and workspace storage

The read-only Docker inventory reported:

- Build cache: **13.06 GB**.
- Docker writable container layers: **4.339 GB**, with **4.339 GB
  reclaimable** reported; these were mostly unrelated stopped containers.
- Docker image layers: **7.896 GB** in the local inventory, with 215.9 MB
  reported reclaimable under shared-layer accounting.
- All local volumes: **3.258 GB**, with 282 MB reported reclaimable; the
  Bet9ja development volume is the 2.327 GB portion described below.
- Bet9ja development PostgreSQL volume: **2.327 GB**.
- Phase 4.1 disposable PostgreSQL volume: **68.04 MB**.
- Older `vfl-research` PostgreSQL volume: **282 MB**, currently unused.
- The production-relevant images are small relative to the unrelated local
  image inventory: Postgres 16 Alpine was 420 MB, the API image 27.3 MB, and
  the web images 341–353 MB. Docker's shared/unique layer accounting must be
  used before removing any image.
- The host also held unrelated multi-gigabyte images and stopped containers.
  No prune command was run.

The local Windows C: drive had **12.55% free** (64.09 GB of 510.65 GB) when
the monitor ran, below the configured 15% critical threshold. The monitor
correctly exited before deeper checks on its default thresholds; a second
read-only run with test-only lower thresholds confirmed the database, WAL,
Docker, container-status, and container-memory measurements above. This is a
workstation finding, not a Contabo VPS measurement, but it validates that the
alert path is actionable.

The repository itself measured approximately 535 MiB under `apps/` (mostly
ignored dependencies), 141 MiB under `tmp/`, and less than 1 MiB under
generated `artifacts/` at audit time. These are development-workspace costs,
not production database costs. The root API build context already excludes
them, and the new web-context ignore file excludes `node_modules`, `.next`,
logs, and local environment files.

## Growth analysis

### Measured densities

The Phase 3.5 report identifies 23 structurally complete seasons and 5,520
matches. The current historical odds count is 773,343 outcome snapshots, or
about 33,623 outcome rows per completed season, consistent with the Phase 1
sample. Using current physical sizes, a new historical outcome/value plus its
observation evidence costs approximately:

- `odds_snapshots`: 624,025,600 / 778,793 ≈ **801 B per outcome** including
  indexes.
- `odds_snapshot_observations`: 538,951,680 / 779,001 ≈ **692 B per
  observation** including indexes.
- `raw_payloads`: approximately **2 MiB per completed season** at the current
  mix, with PostgreSQL TOAST compression already applied.
- Research rows and catalog/index overhead bring the observed historical
  database growth to roughly **53 MiB per completed season** in this local
  dataset. This is a planning density, not a provider guarantee.

The live rows currently span 5.49 hours of bounded/test activity: 5,658 odds
observations, or approximately 1,030 normalized outcome rows per active hour.
The measured live row and change densities imply about **1.8 KiB per active
live outcome row**, including the odds history and change-history allowance.
This sample is not a sustained season and is therefore used only for scenario
bounds.

### Scenario projection (incremental live evidence only)

The following table assumes the measured 1,030 outcome rows per active hour,
about 1.8 KiB per row, and the indicated number of active hours per day. It
excludes the existing 1.19 GiB database, one-time historical backfills, WAL
recycling headroom, Docker images, and backup files. It is deliberately a
scenario range because no continuous production observation series exists.

| Active monitoring assumption | 30 days | 90 days | 180 days | 365 days |
|---|---:|---:|---:|---:|
| 1 active hour/day | ~0.05 GB | ~0.17 GB | ~0.34 GB | ~0.65 GB |
| 4 active hours/day (planning case) | ~0.22 GB | ~0.67 GB | ~1.34 GB | ~2.61 GB |
| 12 active hours/day (stress case) | ~0.67 GB | ~2.01 GB | ~4.02 GB | ~7.83 GB |

The observed live raw bodies were 14 payloads totalling 2.56 MiB logical,
including five odds payloads averaging 253 KiB. They add a smaller but nonzero
component and should be included in the monitoring trend. A controlled
expansion from 23 to 50 completed seasons would add approximately 27 × 53 MiB
≈ **1.4 GiB** of database storage before WAL headroom. Even the planning
case plus that expansion remains comfortably below a 100 GB disk, provided
Docker build cache and unrelated images are not allowed to accumulate on the
VPS.

## Changes implemented

### Migration 010

`db/migrations/010_phase42_storage_policy.sql` adds:

- `storage_retention_policies` with the four required data classes:
  `permanent`, `short_term_operational`, `compressible_archiveable`, and
  `regenerable`.
- `storage_audit_snapshots` for database/WAL/filesystem/Docker measurements
  captured by an operator or scheduled monitor.
- Conservative autovacuum scale factors (`0.05` vacuum and `0.02` analyze)
  for raw payloads, odds snapshots, odds observations, odds changes, and job
  execution history.
- Seed policies that retain canonical source/evidence permanently, leave raw
  and unverified live archive actions disabled until an auditable archive
  exists, and expose only operational cleanup as dry-run candidates.

The migration is idempotent at the object/seed level and does not delete,
archive, rewrite, partition, or compress existing rows.

### Tools and checks

- `cmd/phase42-storage-audit`: read-only PostgreSQL catalog/WAL/row audit;
  optional read-only Docker inventory; optional explicit `-persist` for a
  database-level audit snapshot.
- `cmd/phase42-retention-dry-run`: reports policy candidates and estimated
  bytes. It contains no apply/delete mode.
- `scripts/phase42-storage-audit.ps1` and
  `scripts/phase42-retention-dry-run.ps1` for local operators.
- `scripts/phase42-storage-check.ps1` and
  `scripts/phase42-storage-check.sh` for free-space/WAL/Docker checks. The
  shell check warns below 25% free disk and fails below 15%; the audit command
  fails when WAL exceeds 2 GiB by default. The Docker portion reports
  container status/restart evidence and one-shot CPU/memory usage when the
  runtime supports `docker stats`.
- `scripts/postgres-logical-backup.sh` and `.ps1` create custom-format dumps
  and verify them with `pg_restore --list`.
- `scripts/postgres-logical-restore.sh` is explicitly guarded by
  `CONFIRM_RESTORE=YES` and was not run.
- `apps/web/.dockerignore` prevents local frontend dependencies/build output
  from inflating the web build context.

## Retention and archiving policy

| Class | Data | Phase 4.2 policy |
|---|---|---|
| Permanent | Seasons, teams, fixtures/results; verified pre-cutoff odds; frozen predictions/revisions/settlements; research artifacts; auth audit events | Retain indefinitely |
| Short-term operational | Job execution history; storage audit snapshots | Dry-run candidates after 90/365 days; no deletion performed |
| Compressible/archiveable | Raw provider payloads; unverified live odds; retired candidate artifacts | Archive only after lossless, checksum-verified, provenance-preserving storage exists; disabled by default |
| Regenerable | Expired magic-link tokens and stale rate-limit counters | Dry-run candidates after 30 days; no deletion performed |

Raw payloads cannot currently be deleted safely because normalized records,
observations, quarantine, and provenance rows reference them. A future archive
must preserve payload hash, endpoint/request key, provider timestamps, local
observation/fetch times, parser/schema versions, and the foreign-key lineage
before any PostgreSQL removal is considered. No object-storage integration was
added: its credentials, cost, lifecycle policy, and restore test would need a
separate decision.

## VPS resource recommendations

For Contabo Cloud VPS 4 (4 vCPU / 8 GB RAM / 100 GB SSD), reserve roughly
1.5–2 GB for the OS, Docker, reverse proxy, and filesystem cache. A safe
initial budget is:

- PostgreSQL: 1.5–2 GB container ceiling; `shared_buffers` around 1–1.5 GB,
  `work_mem` 4–8 MB, `maintenance_work_mem` around 256 MB, and a connection
  pool/`max_connections` target around 50–64. Tune from actual queries before
  increasing.
- Go API plus live/prediction/settlement workers: 1–1.5 GB combined. Keep
  worker concurrency bounded and independent of dashboard sessions.
- Next.js/Caddy: 512 MB–1 GB combined for the small private workload.
- Python inference: 512 MB–1 GB; keep LightGBM training out of the live
  worker budget, one thread by default, with an explicit approximately 2 GB
  batch ceiling or a separate training host.
- Leave at least 2 GB uncommitted memory for spikes, PostgreSQL maintenance,
  and Docker operations. Do not run model training automatically on the VPS.

These are starting limits, not a claim that they have been load-tested on
Contabo. The next deployment phase should apply container limits in staging,
observe RSS/CPU/IO, and adjust from measurements.

## Monitoring and backup conclusions

Use `phase42-storage-check.sh` from a systemd timer or cron job with a protected
`DATABASE_URL`. Capture the JSON output and alert on:

- filesystem free space below 25% (warning) or 15% (critical);
- PostgreSQL WAL above 2 GiB or a persistent replication-slot hold;
- database/volume growth above the recorded trend;
- Docker build cache/image growth above the VPS budget;
- PostgreSQL health failures, connection exhaustion, container restarts, and
  failed logical-backup verification.

The Contabo Auto Backup documentation says the add-on stores daily backups
off-server, keeps up to 10 backups for a maximum of 10 days, and restores the
whole VPS, overwriting current VPS data; individual-file restore is not
offered. Contabo also states that backup accessibility is provided but
recoverability is not guaranteed and recommends regular verification. The
actual add-on status for the future VPS was not accessible from this local
execution environment, so it must be confirmed in the Contabo control panel.
A logical PostgreSQL dump remains useful for database-level recovery, but it
should be copied to a separately protected location; the repository does not
integrate object storage or a second full-server backup system in this phase.

Source: [Contabo VPS Auto Backup documentation](https://docs.contabo.com/docs/servers-hosting/vps-auto-backup/).

## Verification

Passed:

- `go test -p 1 -count=1 ./services/storageaudit ./cmd/phase42-storage-audit ./cmd/phase42-retention-dry-run`
- PostgreSQL integration test
  `TestStorageAuditAndRetentionPlanIntegration` against the disposable Phase
  4.1 PostgreSQL database.
- Migration 010 applied successfully to the disposable database.
- Retention dry-run returned `DRY RUN ONLY` and performed no data mutation.
- Read-only audit command completed against the existing main local database.
- Docker inventory, volume mounts, and PostgreSQL relation/WAL queries
  completed read-only.
- A custom-format logical dump was listed with `pg_restore`, restored into a
  temporary disposable database, checked for the migration catalog, and then
  the temporary database/dump were removed. The main development database was
  not part of this restore test.

Run the phase verifier:

```powershell
./scripts/verify-phase42.ps1
```

With a dedicated disposable PostgreSQL database:

```powershell
./scripts/verify-phase42.ps1 -DatabaseUrl $env:PHASE42_DATABASE_URL -IncludeDocker
```

The generated JSON/Markdown reports live under ignored `artifacts/` paths and
are not committed.

## Known limitations and Phase 5 recommendations

- No sustained Contabo run or production filesystem measurement was possible.
- The live sample spans only a bounded local/test window, so the growth table
  is scenario analysis rather than a production rate forecast.
- Docker build cache and unrelated local images were measured but not pruned.
- Migration 010 is verified on a disposable database; it has not been applied
  to the main development database by this audit.
- No archive target or destructive cleanup implementation exists, by design.
- No Prometheus/SMTP alert delivery was added; the monitor exits with a useful
  status and is ready for operator scheduling.
- PostgreSQL `VACUUM FULL`, index rebuilds, partitioning, raw-body compression
  rewrites, and external object storage are deferred until a production
  baseline and recovery target are approved.

Before Phase 5 deployment: apply migration 010 in staging, run a logical dump
and restore into an isolated PostgreSQL instance, configure the storage check
timer, confirm the Contabo Auto Backup add-on and its actual retention in the
control panel, remove unrelated development Docker caches from the VPS image,
and establish a 7–14 day storage/WAL trend before enabling any archive or
cleanup action.
