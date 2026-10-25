BEGIN;

-- Phase 4.2 records storage policy as data, but deliberately does not perform
-- cleanup.  The dry-run tool is the only consumer in this phase.  Applying a
-- retention action requires a separately approved archive or deletion job.
CREATE TABLE IF NOT EXISTS storage_retention_policies (
    policy_key text PRIMARY KEY,
    data_class text NOT NULL CHECK (data_class IN (
        'permanent',
        'short_term_operational',
        'compressible_archiveable',
        'regenerable'
    )),
    retention_days integer CHECK (retention_days IS NULL OR retention_days >= 0),
    action text NOT NULL CHECK (action IN ('retain', 'archive', 'delete')),
    enabled boolean NOT NULL DEFAULT true,
    scope jsonb NOT NULL DEFAULT '{}'::jsonb,
    description text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS storage_audit_snapshots (
    storage_audit_snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    collected_at timestamptz NOT NULL,
    database_name text,
    database_bytes bigint CHECK (database_bytes IS NULL OR database_bytes >= 0),
    data_directory_bytes bigint CHECK (data_directory_bytes IS NULL OR data_directory_bytes >= 0),
    wal_bytes bigint CHECK (wal_bytes IS NULL OR wal_bytes >= 0),
    filesystem_total_bytes bigint CHECK (filesystem_total_bytes IS NULL OR filesystem_total_bytes >= 0),
    filesystem_free_bytes bigint CHECK (filesystem_free_bytes IS NULL OR filesystem_free_bytes >= 0),
    docker_volume_bytes bigint CHECK (docker_volume_bytes IS NULL OR docker_volume_bytes >= 0),
    docker_image_bytes bigint CHECK (docker_image_bytes IS NULL OR docker_image_bytes >= 0),
    docker_build_cache_bytes bigint CHECK (docker_build_cache_bytes IS NULL OR docker_build_cache_bytes >= 0),
    measurements jsonb NOT NULL DEFAULT '{}'::jsonb,
    source text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS storage_audit_snapshots_collected_idx
    ON storage_audit_snapshots (collected_at DESC);

-- Defaults are conservative: canonical source/evidence remains permanent;
-- operational cleanup is visible to dry-run reporting; archive/delete is not
-- executed by this migration or by the Phase 4.2 verifier.
INSERT INTO storage_retention_policies
    (policy_key, data_class, retention_days, action, enabled, scope, description)
VALUES
    ('fixtures_results_and_teams', 'permanent', NULL, 'retain', true, '{}',
     'Canonical seasons, teams, fixtures, and final results are research evidence.'),
    ('verified_pre_cutoff_odds', 'permanent', NULL, 'retain', true,
     '{"point_in_time_verified":true}',
     'Verified pre-cutoff odds and their observation provenance must not be removed.'),
    ('frozen_predictions_and_settlements', 'permanent', NULL, 'retain', true, '{}',
     'Frozen predictions, revisions, corrections, and settlement evidence are immutable research records.'),
    ('raw_provider_payloads', 'compressible_archiveable', NULL, 'archive', false, '{}',
     'Raw provider bodies remain in PostgreSQL until an auditable, lossless archive is approved.'),
    ('unverified_live_observations', 'compressible_archiveable', 365, 'archive', false,
     '{"point_in_time_verified":false}',
     'Unverified live observations may be archived only after provenance-preserving archive storage exists.'),
    ('research_artifacts', 'permanent', NULL, 'retain', true, '{}',
     'Feature snapshots, research predictions, evaluations, and betting-analysis records remain reproducible.'),
    ('auth_audit_events', 'permanent', NULL, 'retain', true, '{}',
     'Authentication audit events support security review and are not disposable telemetry.'),
    ('retired_model_candidates', 'compressible_archiveable', 365, 'archive', false,
     '{"promotion_status":"retired"}',
     'Retired model candidates may be archived only after artifact checksums and restore procedures are verified.'),
    ('job_execution_history', 'short_term_operational', 90, 'delete', true, '{}',
     'Completed operational job history is retained for diagnosis and then becomes a dry-run cleanup candidate.'),
    ('ingestion_failures_and_validation', 'short_term_operational', 365, 'retain', true, '{}',
     'Failures and quarantine records remain available for audit and remediation.'),
    ('auth_ephemeral_tokens_and_rate_limits', 'regenerable', 30, 'delete', true, '{}',
     'Expired magic-link tokens and stale rate-limit rows are regenerable operational data.'),
    ('storage_audit_snapshots', 'short_term_operational', 365, 'delete', true, '{}',
     'Storage measurements are retained long enough for trend analysis and alert diagnosis.')
ON CONFLICT (policy_key) DO NOTHING;

-- These are append-heavy evidence tables.  Lower analyze thresholds keep
-- storage/coverage queries current; autovacuum remains PostgreSQL-managed.
ALTER TABLE raw_payloads
    SET (autovacuum_vacuum_scale_factor = 0.05,
         autovacuum_analyze_scale_factor = 0.02);
ALTER TABLE odds_snapshots
    SET (autovacuum_vacuum_scale_factor = 0.05,
         autovacuum_analyze_scale_factor = 0.02);
ALTER TABLE odds_snapshot_observations
    SET (autovacuum_vacuum_scale_factor = 0.05,
         autovacuum_analyze_scale_factor = 0.02);
ALTER TABLE odds_change_history
    SET (autovacuum_vacuum_scale_factor = 0.05,
         autovacuum_analyze_scale_factor = 0.02);
ALTER TABLE job_execution_history
    SET (autovacuum_vacuum_scale_factor = 0.05,
         autovacuum_analyze_scale_factor = 0.02);

INSERT INTO schema_migrations (version)
VALUES ('010_phase42_storage_policy')
ON CONFLICT (version) DO NOTHING;

COMMIT;
