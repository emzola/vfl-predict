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

`016_phase7_flat_multi_betting.sql` is retained as historical ledger
compatibility for the earlier fixture-level auto-betting shape. It is not a
description of the current selection or staking behavior.

`017_phase7_martingale_settlement.sql` is also historical. It records the
minimum-odds and settlement-gated behavior used before the current Odd/even
strategy and is not rewritten after application.

`018_phase7_odd_even_betting.sql` switches new auto-betting ledger rows to the
Odd/even market's even outcome, removes the legacy 1.20/round-25 quote gate,
and enforces one new-strategy decision per season round while preserving older
ledger rows unchanged.

`019_phase7_flat_staking.sql` records the short-lived flat-staking behavior
used after the Odd/even switch. Its historical ledger rows remain unchanged.

`020_phase7_martingale_recovery.sql` makes the active recovery policy explicit:
the worker waits for settlement and sizes each winning recovery bet to cover
all outstanding principal losses, missed profits, and the current base-profit
target. Existing ledger rows remain unchanged.

`021_phase7_cross_season_recovery.sql` adds the cutoff used by the service to
keep recovery across season boundaries. The service refreshes that cutoff when
auto betting is turned off or re-enabled, so re-enabling starts a fresh cycle.

`022_phase7_over_1_5_market_gate.sql` records the temporary Over 1.5 market
gate. It selected one match per round using the highest normalized observed 1X2
home or away probability strictly above 60%, while preserving historical
ledger rows.

`023_phase7_odd_even_flat_staking.sql` restores the Odd/even market with the
same observed 1X2 qualification and one-match-per-round selection. It is
historical and is superseded by the later recovery-strategy migrations.

`024_phase7_odd_even_four_step_cycle.sql` initializes the current four-step
cycle at even without changing historical ledger rows. It is also historical.

`025_phase7_double_chance_12_martingale.sql` makes Double Chance 12 the active
market, restores recovery martingale staking, and starts a fresh recovery cycle
while preserving season-spanning recovery and one active decision per round. It
is superseded by the current Odd/even strategy.

`026_phase7_odd_even_six_step_cycle.sql` restores the Odd/even market with the
#1 prediction ranking and one-match-per-round selection. It is superseded by
the eight-step cycle migration below; martingale recovery continues from the
existing cutoff across market and season changes.

`027_phase7_odd_even_eight_step_cycle.sql` changes new Odd/even bets to the
repeating odd, even, odd, even, odd, odd, even, even cycle without resetting
the recovery balance or changing historical ledger rows.

`028_phase7_odd_even_ten_step_cycle.sql` changes new Odd/even bets to the
repeating odd, even, odd, even, even, odd, even, even, odd, odd cycle without
resetting the recovery balance or changing historical ledger rows.
