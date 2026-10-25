# Database migrations

`001_phase1_initial.sql` creates the normalized Phase 1 schema. It is applied by
`cmd/historical-ingest` before a run and can be applied independently with any
PostgreSQL 16 client. It stores provider timestamps separately from HTTP
observation/ingestion timestamps and retains raw JSON for audit and reprocessing.

The migration is intentionally idempotent at the object and version-record
level. Use a dedicated local database for integration tests; do not point the
test suite at production data.
