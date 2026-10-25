BEGIN;

-- Phase 3 operational records are deliberately separate from retrospective
-- research artifacts.  A feed observation and its fetched_at timestamp are
-- evidence of what this application saw; provider timestamps are retained as
-- separate fields and are never promoted to observation time.

CREATE TABLE IF NOT EXISTS live_feed_observations (
    observation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_family text NOT NULL,
    request_key text NOT NULL,
    request_url text NOT NULL,
    response_status integer NOT NULL CHECK (response_status BETWEEN 100 AND 599),
    fetched_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    provider_born_uts bigint,
    provider_born_at timestamptz,
    provider_timestamp_ms bigint,
    provider_timestamp_at timestamptz,
    payload_sha256 char(64) NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    raw_payload_id uuid NOT NULL REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT,
    response_bytes integer NOT NULL CHECK (response_bytes >= 0),
    cache_reused boolean NOT NULL DEFAULT false,
    cache_control text,
    etag text,
    last_modified text,
    season_id text REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint CHECK (round_no IS NULL OR round_no BETWEEN 1 AND 30),
    parse_status text NOT NULL CHECK (parse_status IN ('accepted', 'not_modified', 'invalid', 'unavailable')),
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (raw_payload_id, endpoint_family, request_key)
);

CREATE INDEX IF NOT EXISTS live_feed_observations_lookup_idx
    ON live_feed_observations (endpoint_family, request_key, observed_at DESC);
CREATE INDEX IF NOT EXISTS live_feed_observations_freshness_idx
    ON live_feed_observations (season_id, round_no, observed_at DESC);

CREATE TABLE IF NOT EXISTS odds_change_history (
    odds_change_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    provider_market_id bigint NOT NULL CHECK (provider_market_id > 0),
    specifiers text NOT NULL DEFAULT '',
    outcome_id text NOT NULL,
    previous_odds_snapshot_id uuid REFERENCES odds_snapshots (odds_snapshot_id) ON DELETE RESTRICT,
    current_odds_snapshot_id uuid NOT NULL REFERENCES odds_snapshots (odds_snapshot_id) ON DELETE RESTRICT,
    previous_decimal_odds numeric(18, 8),
    current_decimal_odds numeric(18, 8) NOT NULL CHECK (current_decimal_odds > 0),
    observed_at timestamptz NOT NULL,
    change_type text NOT NULL CHECK (change_type IN ('initial', 'price_changed', 'availability_changed')),
    meaningful boolean NOT NULL DEFAULT true,
    raw_payload_id uuid REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (current_odds_snapshot_id, previous_odds_snapshot_id, change_type)
);

CREATE INDEX IF NOT EXISTS odds_change_history_match_idx
    ON odds_change_history (match_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS round_states (
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    earliest_kickoff_at timestamptz NOT NULL,
    betting_cutoff_at timestamptz,
    state text NOT NULL CHECK (state IN ('pending', 'provisional', 'frozen', 'settled', 'stale', 'unavailable', 'missed_cutoff')),
    last_fixture_observed_at timestamptz,
    last_odds_observed_at timestamptz,
    settled_at timestamptz,
    freshness jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (season_id, round_no)
);

CREATE INDEX IF NOT EXISTS round_states_schedule_idx
    ON round_states (state, earliest_kickoff_at);

CREATE TABLE IF NOT EXISTS prediction_runs (
    prediction_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    model_version text NOT NULL,
    run_type text NOT NULL CHECK (run_type IN ('provisional', 'frozen', 'forward', 'manual')),
    status text NOT NULL CHECK (status IN ('pending', 'provisional', 'frozen', 'settled', 'stale', 'unavailable', 'missed_cutoff')),
    prediction_as_of timestamptz NOT NULL,
    input_cutoff_at timestamptz NOT NULL,
    earliest_kickoff_at timestamptz NOT NULL,
    odds_observation_cutoff_at timestamptz,
    input_dataset_version text NOT NULL,
    prediction_policy text NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    expected_fixture_count smallint NOT NULL DEFAULT 8 CHECK (expected_fixture_count = 8),
    generated_fixture_count smallint NOT NULL DEFAULT 0 CHECK (generated_fixture_count BETWEEN 0 AND 8),
    frozen_at timestamptz,
    failure_reason text,
    manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (season_id, round_no, model_version, input_dataset_version, prediction_policy, run_type, prediction_as_of)
);

CREATE INDEX IF NOT EXISTS prediction_runs_round_idx
    ON prediction_runs (season_id, round_no, prediction_as_of DESC);
CREATE INDEX IF NOT EXISTS prediction_runs_status_idx
    ON prediction_runs (status, earliest_kickoff_at);

CREATE TABLE IF NOT EXISTS prediction_revisions (
    prediction_revision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prediction_run_id uuid NOT NULL REFERENCES prediction_runs (prediction_run_id) ON DELETE RESTRICT,
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    revision_no integer NOT NULL CHECK (revision_no > 0),
    status text NOT NULL CHECK (status IN ('pending', 'provisional', 'frozen', 'settled', 'stale', 'unavailable', 'missed_cutoff')),
    prediction_as_of timestamptz NOT NULL,
    cutoff_at timestamptz NOT NULL,
    home_probability numeric(18, 12) NOT NULL CHECK (home_probability BETWEEN 0 AND 1),
    draw_probability numeric(18, 12) NOT NULL CHECK (draw_probability BETWEEN 0 AND 1),
    away_probability numeric(18, 12) NOT NULL CHECK (away_probability BETWEEN 0 AND 1),
    probability_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    odds_snapshot_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    odds_observed_at timestamptz,
    provider_odds_timestamp_at timestamptz,
    point_in_time_verified boolean NOT NULL DEFAULT false,
    eligible boolean NOT NULL DEFAULT false,
    ineligible_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    frozen_at timestamptz,
    CHECK (abs((home_probability + draw_probability + away_probability) - 1) < 0.000001),
    UNIQUE (prediction_run_id, match_id),
    UNIQUE (prediction_run_id, match_id, revision_no)
);

CREATE INDEX IF NOT EXISTS prediction_revisions_lookup_idx
    ON prediction_revisions (season_id, round_no, status, created_at DESC);

CREATE TABLE IF NOT EXISTS prediction_settlements (
    prediction_settlement_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prediction_revision_id uuid NOT NULL REFERENCES prediction_revisions (prediction_revision_id) ON DELETE RESTRICT,
    correction_version integer NOT NULL CHECK (correction_version > 0),
    settlement_track text NOT NULL CHECK (settlement_track IN ('forward_observed', 'historical_retrospective')),
    final_home_score integer NOT NULL CHECK (final_home_score >= 0),
    final_away_score integer NOT NULL CHECK (final_away_score >= 0),
    actual_outcome text NOT NULL CHECK (actual_outcome IN ('home', 'draw', 'away')),
    log_loss numeric(24, 12) NOT NULL,
    brier_score numeric(24, 12) NOT NULL,
    correct boolean NOT NULL,
    quoted_decimal_odds numeric(18, 8),
    hypothetical_stake numeric(18, 8),
    hypothetical_return numeric(18, 8),
    hypothetical_profit numeric(18, 8),
    evaluated_at timestamptz NOT NULL,
    result_observed_at timestamptz NOT NULL,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (prediction_revision_id, correction_version)
);

CREATE INDEX IF NOT EXISTS prediction_settlements_track_idx
    ON prediction_settlements (settlement_track, evaluated_at DESC);

CREATE TABLE IF NOT EXISTS forward_paper_results (
    forward_result_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prediction_run_id uuid NOT NULL REFERENCES prediction_runs (prediction_run_id) ON DELETE RESTRICT,
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    market_key text NOT NULL,
    metric_key text NOT NULL,
    metric_value numeric(24, 12) NOT NULL,
    sample_count integer NOT NULL CHECK (sample_count >= 0),
    eligible_odds_count integer NOT NULL DEFAULT 0 CHECK (eligible_odds_count >= 0),
    odds_policy text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (prediction_run_id, market_key, metric_key)
);

CREATE TABLE IF NOT EXISTS scheduler_leases (
    lease_key text PRIMARY KEY,
    owner_id text NOT NULL,
    lease_token uuid NOT NULL DEFAULT gen_random_uuid(),
    acquired_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz NOT NULL,
    heartbeat_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS scheduler_leases_expiry_idx
    ON scheduler_leases (lease_until);

CREATE TABLE IF NOT EXISTS provider_request_budget (
    budget_key text PRIMARY KEY,
    next_allowed_at timestamptz NOT NULL,
    last_reserved_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS provider_request_budget_updated_idx
    ON provider_request_budget (updated_at);

CREATE TABLE IF NOT EXISTS job_execution_history (
    execution_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_key text NOT NULL,
    worker_name text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'skipped', 'cancelled')),
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    response_count integer NOT NULL DEFAULT 0 CHECK (response_count >= 0),
    cache_hit_count integer NOT NULL DEFAULT 0 CHECK (cache_hit_count >= 0),
    error_code text,
    error_message text,
    retryable boolean,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    CHECK (finished_at IS NULL OR finished_at >= started_at)
);

CREATE INDEX IF NOT EXISTS job_execution_history_job_idx
    ON job_execution_history (job_key, started_at DESC);

CREATE OR REPLACE FUNCTION prevent_frozen_prediction_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'frozen' THEN
            RAISE EXCEPTION 'frozen prediction records are immutable';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status = 'frozen' AND (
        NEW.prediction_run_id IS DISTINCT FROM OLD.prediction_run_id OR
        NEW.season_id IS DISTINCT FROM OLD.season_id OR
        NEW.match_id IS DISTINCT FROM OLD.match_id OR
        NEW.round_no IS DISTINCT FROM OLD.round_no OR
        NEW.revision_no IS DISTINCT FROM OLD.revision_no OR
        NEW.prediction_as_of IS DISTINCT FROM OLD.prediction_as_of OR
        NEW.cutoff_at IS DISTINCT FROM OLD.cutoff_at OR
        NEW.home_probability IS DISTINCT FROM OLD.home_probability OR
        NEW.draw_probability IS DISTINCT FROM OLD.draw_probability OR
        NEW.away_probability IS DISTINCT FROM OLD.away_probability OR
        NEW.probability_payload IS DISTINCT FROM OLD.probability_payload OR
        NEW.odds_snapshot_ids IS DISTINCT FROM OLD.odds_snapshot_ids OR
        NEW.odds_observed_at IS DISTINCT FROM OLD.odds_observed_at OR
        NEW.provider_odds_timestamp_at IS DISTINCT FROM OLD.provider_odds_timestamp_at OR
        NEW.point_in_time_verified IS DISTINCT FROM OLD.point_in_time_verified OR
        NEW.eligible IS DISTINCT FROM OLD.eligible
    ) THEN
        RAISE EXCEPTION 'frozen prediction records are immutable';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS prediction_revisions_frozen_guard ON prediction_revisions;
CREATE TRIGGER prediction_revisions_frozen_guard
BEFORE UPDATE OR DELETE ON prediction_revisions
FOR EACH ROW EXECUTE FUNCTION prevent_frozen_prediction_mutation();

CREATE OR REPLACE FUNCTION prevent_frozen_prediction_run_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'frozen' THEN
            RAISE EXCEPTION 'frozen prediction runs are immutable';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status = 'frozen' AND (
        NEW.season_id IS DISTINCT FROM OLD.season_id OR
        NEW.round_no IS DISTINCT FROM OLD.round_no OR
        NEW.model_definition_id IS DISTINCT FROM OLD.model_definition_id OR
        NEW.model_version IS DISTINCT FROM OLD.model_version OR
        NEW.run_type IS DISTINCT FROM OLD.run_type OR
        NEW.prediction_as_of IS DISTINCT FROM OLD.prediction_as_of OR
        NEW.input_cutoff_at IS DISTINCT FROM OLD.input_cutoff_at OR
        NEW.earliest_kickoff_at IS DISTINCT FROM OLD.earliest_kickoff_at OR
        NEW.input_dataset_version IS DISTINCT FROM OLD.input_dataset_version OR
        NEW.prediction_policy IS DISTINCT FROM OLD.prediction_policy OR
        NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key OR
        NEW.expected_fixture_count IS DISTINCT FROM OLD.expected_fixture_count OR
        NEW.generated_fixture_count IS DISTINCT FROM OLD.generated_fixture_count OR
        NEW.manifest IS DISTINCT FROM OLD.manifest
    ) THEN
        RAISE EXCEPTION 'frozen prediction runs are immutable';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS prediction_runs_frozen_guard ON prediction_runs;
CREATE TRIGGER prediction_runs_frozen_guard
BEFORE UPDATE OR DELETE ON prediction_runs
FOR EACH ROW EXECUTE FUNCTION prevent_frozen_prediction_run_mutation();

INSERT INTO schema_migrations (version)
VALUES ('004_phase3_operational')
ON CONFLICT (version) DO NOTHING;

COMMIT;
