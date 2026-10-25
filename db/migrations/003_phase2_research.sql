BEGIN;

-- Phase 2 research artifacts are append-only, versioned records.  They are
-- deliberately separate from the Phase 1 ingestion tables so that a feature
-- or model revision never mutates the source data used by an earlier run.

CREATE TABLE IF NOT EXISTS feature_definitions (
    feature_definition_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feature_key text NOT NULL,
    version text NOT NULL,
    description text NOT NULL,
    point_in_time_policy text NOT NULL,
    definition jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (feature_key, version)
);

CREATE TABLE IF NOT EXISTS feature_snapshots (
    feature_snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    feature_definition_id bigint NOT NULL REFERENCES feature_definitions (feature_definition_id) ON DELETE RESTRICT,
    dataset_fingerprint char(64) NOT NULL CHECK (dataset_fingerprint ~ '^[0-9a-f]{64}$'),
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    cutoff_at timestamptz NOT NULL,
    generated_at timestamptz NOT NULL,
    values jsonb NOT NULL,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    point_in_time_verified boolean NOT NULL DEFAULT false,
    UNIQUE (feature_definition_id, dataset_fingerprint, match_id, cutoff_at)
);

CREATE INDEX IF NOT EXISTS feature_snapshots_lookup_idx
    ON feature_snapshots (season_id, round_no, cutoff_at, feature_definition_id);

CREATE TABLE IF NOT EXISTS model_definitions (
    model_definition_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    model_key text NOT NULL,
    version text NOT NULL,
    description text NOT NULL,
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (model_key, version)
);

CREATE TABLE IF NOT EXISTS training_runs (
    training_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    dataset_fingerprint char(64) NOT NULL CHECK (dataset_fingerprint ~ '^[0-9a-f]{64}$'),
    train_through_season_id text REFERENCES seasons (season_id) ON DELETE RESTRICT,
    train_through_round smallint CHECK (train_through_round IS NULL OR train_through_round BETWEEN 1 AND 30),
    configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    random_seed bigint,
    status text NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    code_version text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);

CREATE INDEX IF NOT EXISTS training_runs_model_idx
    ON training_runs (model_definition_id, started_at DESC);

CREATE TABLE IF NOT EXISTS backtest_runs (
    backtest_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset_fingerprint char(64) NOT NULL CHECK (dataset_fingerprint ~ '^[0-9a-f]{64}$'),
    configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    cutoff_policy text NOT NULL,
    odds_provenance_policy text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    code_version text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);

CREATE INDEX IF NOT EXISTS backtest_runs_dataset_idx
    ON backtest_runs (dataset_fingerprint, started_at DESC);

CREATE TABLE IF NOT EXISTS research_predictions (
    prediction_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    backtest_run_id uuid REFERENCES backtest_runs (backtest_run_id) ON DELETE RESTRICT,
    training_run_id uuid REFERENCES training_runs (training_run_id) ON DELETE RESTRICT,
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    prediction_as_of timestamptz NOT NULL,
    cutoff_at timestamptz NOT NULL,
    actual_outcome text CHECK (actual_outcome IS NULL OR actual_outcome IN ('home', 'draw', 'away')),
    home_probability numeric(18, 12) NOT NULL CHECK (home_probability BETWEEN 0 AND 1),
    draw_probability numeric(18, 12) NOT NULL CHECK (draw_probability BETWEEN 0 AND 1),
    away_probability numeric(18, 12) NOT NULL CHECK (away_probability BETWEEN 0 AND 1),
    probability_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    feature_definition_id bigint REFERENCES feature_definitions (feature_definition_id) ON DELETE RESTRICT,
    odds_observed_at timestamptz,
    provider_odds_timestamp_at timestamptz,
    odds_provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    point_in_time_verified boolean NOT NULL DEFAULT false,
    model_version text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (abs((home_probability + draw_probability + away_probability) - 1) < 0.000001),
    CHECK (backtest_run_id IS NOT NULL OR training_run_id IS NOT NULL),
    UNIQUE (backtest_run_id, model_definition_id, match_id, prediction_as_of)
);

CREATE INDEX IF NOT EXISTS research_predictions_match_idx
    ON research_predictions (season_id, round_no, match_id, prediction_as_of);

CREATE TABLE IF NOT EXISTS evaluation_metrics (
    evaluation_metric_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    backtest_run_id uuid NOT NULL REFERENCES backtest_runs (backtest_run_id) ON DELETE CASCADE,
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    scope_type text NOT NULL CHECK (scope_type IN ('aggregate', 'season', 'round', 'season_round', 'market')),
    scope_season_id text REFERENCES seasons (season_id) ON DELETE RESTRICT,
    scope_round_no smallint CHECK (scope_round_no IS NULL OR scope_round_no BETWEEN 1 AND 30),
    market_key text,
    metric_key text NOT NULL,
    metric_value numeric(24, 12) NOT NULL,
    sample_count integer NOT NULL CHECK (sample_count >= 0),
    confidence_low numeric(24, 12),
    confidence_high numeric(24, 12),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (backtest_run_id, model_definition_id, scope_type, scope_season_id, scope_round_no, market_key, metric_key)
);

CREATE INDEX IF NOT EXISTS evaluation_metrics_lookup_idx
    ON evaluation_metrics (backtest_run_id, model_definition_id, scope_type, scope_season_id, scope_round_no);

CREATE TABLE IF NOT EXISTS betting_analysis_runs (
    betting_analysis_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    backtest_run_id uuid REFERENCES backtest_runs (backtest_run_id) ON DELETE RESTRICT,
    dataset_fingerprint char(64) NOT NULL CHECK (dataset_fingerprint ~ '^[0-9a-f]{64}$'),
    configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    odds_provenance_policy text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);

CREATE TABLE IF NOT EXISTS betting_analysis_selections (
    betting_selection_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    betting_analysis_run_id uuid NOT NULL REFERENCES betting_analysis_runs (betting_analysis_run_id) ON DELETE CASCADE,
    prediction_id uuid REFERENCES research_predictions (prediction_id) ON DELETE RESTRICT,
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    outcome_key text NOT NULL CHECK (outcome_key IN ('home', 'draw', 'away')),
    model_probability numeric(18, 12) NOT NULL CHECK (model_probability BETWEEN 0 AND 1),
    quoted_decimal_odds numeric(18, 8) NOT NULL CHECK (quoted_decimal_odds > 0),
    bookmaker_implied_probability numeric(18, 12) NOT NULL CHECK (bookmaker_implied_probability >= 0),
    expected_value numeric(24, 12) NOT NULL,
    stake numeric(18, 8) NOT NULL CHECK (stake >= 0),
    realized_return numeric(18, 8),
    realized_profit numeric(18, 8),
    odds_observed_at timestamptz,
    point_in_time_verified boolean NOT NULL DEFAULT false,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (betting_analysis_run_id, match_id, outcome_key)
);

CREATE INDEX IF NOT EXISTS betting_analysis_selections_season_idx
    ON betting_analysis_selections (betting_analysis_run_id, season_id, round_no);

CREATE TABLE IF NOT EXISTS betting_analysis_metrics (
    betting_metric_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    betting_analysis_run_id uuid NOT NULL REFERENCES betting_analysis_runs (betting_analysis_run_id) ON DELETE CASCADE,
    scope_type text NOT NULL CHECK (scope_type IN ('aggregate', 'season', 'market')),
    scope_season_id text REFERENCES seasons (season_id) ON DELETE RESTRICT,
    market_key text,
    metric_key text NOT NULL,
    metric_value numeric(24, 12) NOT NULL,
    sample_count integer NOT NULL CHECK (sample_count >= 0),
    confidence_low numeric(24, 12),
    confidence_high numeric(24, 12),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (betting_analysis_run_id, scope_type, scope_season_id, market_key, metric_key)
);

INSERT INTO schema_migrations (version)
VALUES ('003_phase2_research')
ON CONFLICT (version) DO NOTHING;

COMMIT;
