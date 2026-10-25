-- Phase 5 migration integrity catalog. The migrator creates this table before
-- applying the ordered batch as well, so this file is safe on both new and
-- already-initialized databases.
CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    migration_name text,
    checksum_sha256 text,
    applied_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE schema_migrations
    ADD COLUMN IF NOT EXISTS migration_name text;

ALTER TABLE schema_migrations
    ADD COLUMN IF NOT EXISTS checksum_sha256 text;

CREATE INDEX IF NOT EXISTS schema_migrations_name_idx
    ON schema_migrations (migration_name);
