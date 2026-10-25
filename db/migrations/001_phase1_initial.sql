BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS raw_payloads (
    raw_payload_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_family text NOT NULL,
    request_key text NOT NULL,
    request_url text NOT NULL,
    request_params jsonb NOT NULL DEFAULT '{}'::jsonb,
    response_status integer NOT NULL CHECK (response_status BETWEEN 100 AND 599),
    content_type text,
    cache_control text,
    etag text,
    last_modified text,
    provider_born_uts bigint,
    provider_born_at timestamptz,
    provider_timestamp_ms bigint,
    provider_timestamp_at timestamptz,
    fetched_at timestamptz NOT NULL,
    last_fetched_at timestamptz NOT NULL,
    ingested_at timestamptz NOT NULL DEFAULT now(),
    payload_sha256 char(64) NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    payload jsonb,
    payload_text text NOT NULL,
    CHECK (payload IS NOT NULL OR payload_text IS NOT NULL),
    parser_version text NOT NULL,
    schema_version text NOT NULL,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (endpoint_family, request_key, payload_sha256)
);

CREATE INDEX IF NOT EXISTS raw_payloads_request_idx
    ON raw_payloads (endpoint_family, request_key, last_fetched_at DESC);

CREATE TABLE IF NOT EXISTS seasons (
    season_id text PRIMARY KEY CHECK (season_id ~ '^[0-9]+$'),
    provider_season_id text NOT NULL UNIQUE,
    tournament_id bigint NOT NULL CHECK (tournament_id > 0),
    provider_name text NOT NULL,
    abbreviation text,
    provider_year text,
    current_flag boolean NOT NULL DEFAULT false,
    starts_at timestamptz,
    ends_at timestamptz,
    discovered_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    raw_payload_id uuid REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS seasons_current_idx ON seasons (current_flag, starts_at DESC);

CREATE TABLE IF NOT EXISTS teams (
    team_uid bigint PRIMARY KEY CHECK (team_uid > 0),
    provider_team_id bigint CHECK (provider_team_id > 0),
    name text NOT NULL,
    medium_name text,
    abbreviation text,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    raw_payload_id uuid REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS fixtures (
    match_id bigint PRIMARY KEY CHECK (match_id > 0),
    provider_match_id text NOT NULL UNIQUE,
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    tournament_id bigint NOT NULL CHECK (tournament_id > 0),
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    scheduled_at timestamptz NOT NULL,
    provider_scheduled_uts bigint NOT NULL CHECK (provider_scheduled_uts > 0),
    home_team_uid bigint NOT NULL REFERENCES teams (team_uid) ON DELETE RESTRICT,
    away_team_uid bigint NOT NULL REFERENCES teams (team_uid) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('scheduled', 'in_progress', 'completed', 'canceled', 'postponed', 'unknown')),
    result_home integer,
    result_away integer,
    half_time_home integer,
    half_time_away integer,
    full_time_home integer,
    full_time_away integer,
    provider_status jsonb,
    postponed boolean NOT NULL DEFAULT false,
    canceled boolean NOT NULL DEFAULT false,
    walkover boolean NOT NULL DEFAULT false,
    to_be_announced boolean NOT NULL DEFAULT false,
    in_live_score boolean NOT NULL DEFAULT false,
    status_observed_at timestamptz NOT NULL,
    finalized_at timestamptz,
    raw_payload_id uuid REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (home_team_uid <> away_team_uid),
    CHECK ((result_home IS NULL) = (result_away IS NULL)),
    CHECK ((half_time_home IS NULL) = (half_time_away IS NULL)),
    CHECK ((full_time_home IS NULL) = (full_time_away IS NULL)),
    UNIQUE (season_id, round_no, match_id)
);

CREATE INDEX IF NOT EXISTS fixtures_season_round_idx ON fixtures (season_id, round_no, match_id);
CREATE INDEX IF NOT EXISTS fixtures_schedule_status_idx ON fixtures (scheduled_at, status);

CREATE TABLE IF NOT EXISTS market_definitions (
    market_definition_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider_market_id bigint NOT NULL CHECK (provider_market_id > 0),
    market_name text NOT NULL,
    UNIQUE (provider_market_id, market_name)
);

CREATE TABLE IF NOT EXISTS odds_snapshots (
    odds_snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    market_definition_id bigint NOT NULL REFERENCES market_definitions (market_definition_id) ON DELETE RESTRICT,
    provider_market_id bigint NOT NULL CHECK (provider_market_id > 0),
    specifiers text NOT NULL DEFAULT '',
    outcome_id text NOT NULL,
    outcome_name text NOT NULL,
    outcome_abbreviation text,
    raw_odds text NOT NULL,
    decimal_odds numeric(18, 8) CHECK (decimal_odds IS NULL OR decimal_odds > 0),
    odds_valid boolean NOT NULL,
    odds_error text,
    market_status integer NOT NULL,
    outcome_active integer NOT NULL,
    provider_result integer NOT NULL,
    source_timestamp_ms bigint NOT NULL CHECK (source_timestamp_ms > 0),
    source_timestamp_at timestamptz NOT NULL,
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    fetched_at timestamptz NOT NULL,
    observation_kind text NOT NULL CHECK (observation_kind IN ('historical_returned_after_settlement', 'live_observation', 'unknown')),
    point_in_time_verified boolean NOT NULL DEFAULT false,
    raw_payload_id uuid NOT NULL REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (match_id, provider_market_id, specifiers, outcome_id, source_timestamp_ms, raw_odds)
);

CREATE INDEX IF NOT EXISTS odds_snapshots_match_observed_idx
    ON odds_snapshots (match_id, last_observed_at DESC);
CREATE INDEX IF NOT EXISTS odds_snapshots_market_lookup_idx
    ON odds_snapshots (match_id, provider_market_id, specifiers, outcome_id, last_observed_at DESC);

CREATE TABLE IF NOT EXISTS ingestion_jobs (
    job_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type text NOT NULL CHECK (job_type IN ('season_discovery', 'season_backfill', 'fixtures_import', 'odds_import')),
    season_id text REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint CHECK (round_no IS NULL OR round_no BETWEEN 1 AND 30),
    idempotency_key text NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'completed_with_errors', 'failed')),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    run_after timestamptz NOT NULL DEFAULT now(),
    leased_until timestamptz,
    checkpoint jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_error text,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ingestion_jobs_queue_idx ON ingestion_jobs (status, run_after);

CREATE TABLE IF NOT EXISTS ingestion_checkpoints (
    checkpoint_key text PRIMARY KEY,
    scope text NOT NULL CHECK (scope IN ('season_discovery', 'season_fixtures', 'season_odds')),
    season_id text REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint CHECK (round_no IS NULL OR round_no BETWEEN 1 AND 30),
    state text NOT NULL CHECK (state IN ('pending', 'running', 'completed', 'completed_with_errors', 'failed')),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    request_count integer NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    expected_count integer,
    accepted_count integer NOT NULL DEFAULT 0,
    quarantined_count integer NOT NULL DEFAULT 0,
    cursor text,
    last_raw_payload_id uuid REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT,
    last_error text,
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ingestion_checkpoints_status_idx ON ingestion_checkpoints (state, updated_at);

CREATE TABLE IF NOT EXISTS ingestion_failures (
    failure_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid REFERENCES ingestion_jobs (job_id) ON DELETE SET NULL,
    checkpoint_key text REFERENCES ingestion_checkpoints (checkpoint_key) ON DELETE SET NULL,
    attempt integer NOT NULL CHECK (attempt >= 0),
    error_code text NOT NULL,
    message text NOT NULL,
    retryable boolean NOT NULL,
    response_status integer,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ingestion_failures_checkpoint_idx
    ON ingestion_failures (checkpoint_key, occurred_at DESC);

CREATE TABLE IF NOT EXISTS validation_errors (
    validation_error_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid REFERENCES ingestion_jobs (job_id) ON DELETE SET NULL,
    raw_payload_id uuid REFERENCES raw_payloads (raw_payload_id) ON DELETE SET NULL,
    entity_type text NOT NULL,
    entity_key text NOT NULL,
    error_code text NOT NULL,
    message text NOT NULL,
    raw_fragment jsonb,
    quarantined boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    UNIQUE (raw_payload_id, entity_type, entity_key, error_code)
);

CREATE INDEX IF NOT EXISTS validation_errors_open_idx
    ON validation_errors (resolved_at, created_at DESC);

CREATE OR REPLACE VIEW v_fixture_coverage AS
SELECT
    season_id,
    round_no,
    count(*)::integer AS fixture_count,
    count(*) FILTER (WHERE result_home IS NOT NULL AND result_away IS NOT NULL)::integer AS scored_fixture_count,
    count(*) FILTER (WHERE status = 'completed')::integer AS completed_fixture_count,
    count(*) FILTER (WHERE status IN ('canceled', 'postponed'))::integer AS void_fixture_count
FROM fixtures
GROUP BY season_id, round_no;

CREATE OR REPLACE VIEW v_odds_coverage AS
SELECT
    f.season_id,
    f.round_no,
    count(DISTINCT f.match_id)::integer AS fixture_count,
    count(DISTINCT o.match_id)::integer AS odds_match_count,
    count(o.odds_snapshot_id)::integer AS outcome_snapshot_count,
    count(o.odds_snapshot_id) FILTER (WHERE o.odds_valid)::integer AS usable_outcome_snapshot_count,
    count(DISTINCT (o.match_id, o.provider_market_id, o.specifiers)) FILTER (WHERE o.odds_snapshot_id IS NOT NULL)::integer AS market_snapshot_count
FROM fixtures f
LEFT JOIN odds_snapshots o ON o.match_id = f.match_id
GROUP BY f.season_id, f.round_no;

INSERT INTO schema_migrations (version)
VALUES ('001_phase1_initial')
ON CONFLICT (version) DO NOTHING;

COMMIT;
