# Database migrations

`001_phase1_initial.sql` creates the normalized Phase 1 schema. It is applied by
`cmd/historical-ingest` before a run and can be applied independently with any
PostgreSQL 16 client. It stores provider timestamps separately from HTTP
observation/ingestion timestamps and retains raw JSON for audit and reprocessing.

The migration is intentionally idempotent at the object and version-record
level. Use a dedicated local database for integration tests; do not point the
test suite at production data.

`003_phase2_research.sql` adds append-only feature, model, training, backtest,
prediction, metric, and research-only betting-analysis storage. It does not
change provider source records or certify retrospective odds as point-in-time
observations.

`004_phase3_operational.sql` adds live feed observations, odds change history,
round states, prediction runs and revisions, frozen-round settlement,
forward-paper metrics, scheduler leases, and job execution history. Frozen
prediction content is protected by database triggers.

`005_phase3_observation_history.sql` keeps repeated cached/304 responses as
separate local observations while retaining raw payload deduplication.

`006_phase3_observation_constraint_cleanup.sql` removes the truncated legacy
constraint name on PostgreSQL installations that already applied migration
004, leaving the observed-at-aware uniqueness rule authoritative.

`007_phase3_odds_observation_history.sql` adds append-only local observation
evidence for normalized odds values and backfills one baseline observation per
existing snapshot.

`008_phase35_model_registry.sql` adds immutable LightGBM/post-processing
artifact records, chronological experiment records, checksummed artifact
metadata, and candidate/approved/active/retired promotion states. Phase 3.5
registration defaults every research artifact to `candidate`; promotion is an
explicit, gated operator action.

`009_phase4_auth.sql` adds the single configured account identity, hashed
single-use magic-link tokens, hashed server-side sessions, CSRF token hashes,
login rate-limit counters, authentication audit events, and dashboard display
preferences. It does not create public registration or password records.

`010_phase42_storage_policy.sql` adds the retention-policy catalog and
storage-audit snapshot table. It also sets measured, conservative autovacuum
scale factors on append-heavy evidence tables. It does not delete, archive,
rewrite, partition, or compress existing rows. Use the Phase 4.2 retention
dry-run command before proposing any cleanup, and keep a logical backup before
any separately approved destructive operation.

`011_phase5_migration_metadata.sql` extends the existing version catalog with
the migration filename and SHA-256 checksum used by the dedicated production
`cmd/migrate` runner. Existing Phase 0–4 rows are adopted with their current
file checksums on the first Phase 5 run; a later change to an applied file is
rejected. PostgreSQL advisory locking remains held for the ordered batch.

`012_phase6_dynamic_bayesian.sql` adds the unified dynamic Bayesian scoreline
model's checkpoints, shadow runs, and registry-selection tables. The normal
production deployment runs replay, shadow, and approval gates before selecting
the model; it does not bypass them.

`016_phase7_flat_multi_betting.sql` updates the auto-betting ledger for
fixture-level compatibility while the active selection logic chooses only the
highest-ranked frozen prediction per round. Auto betting uses odds-aware
martingale staking, accepts Over 1.5 odds at least 1.20, and waits for each
bet to settle before processing the next frozen round.

`017_phase7_martingale_settlement.sql` restores the 1.20 minimum-odds default
for new auto-betting ledger rows and records the settlement-gated staking
behavior used by the worker.
