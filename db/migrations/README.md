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
