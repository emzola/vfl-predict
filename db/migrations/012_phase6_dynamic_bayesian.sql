BEGIN;

-- Phase 6 adds a built-in, versioned candidate without changing or rewriting
-- historical prediction records. It is deliberately not active by default.
ALTER TABLE model_artifacts
    DROP CONSTRAINT IF EXISTS model_artifacts_algorithm_check;
ALTER TABLE model_artifacts
    ADD CONSTRAINT model_artifacts_algorithm_check
    CHECK (algorithm IN ('baseline', 'lightgbm', 'dynamic_bayesian'));

ALTER TABLE model_experiments
    DROP CONSTRAINT IF EXISTS model_experiments_algorithm_check;
ALTER TABLE model_experiments
    ADD CONSTRAINT model_experiments_algorithm_check
    CHECK (algorithm IN ('lightgbm', 'baseline', 'calibration', 'ensemble', 'dynamic_bayesian'));

ALTER TABLE model_artifacts
    ADD COLUMN IF NOT EXISTS approved_by text,
    ADD COLUMN IF NOT EXISTS approval_reason text;

-- Deterministic replay checkpoints are audit records, not mutable live model
-- state. A worker can always rebuild from source results, while the checkpoint
-- captures exactly what was used for a prediction run.
CREATE TABLE IF NOT EXISTS dynamic_model_checkpoints (
    checkpoint_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    cutoff_at timestamptz NOT NULL,
    input_dataset_version text NOT NULL,
    state jsonb NOT NULL,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (model_definition_id, season_id, round_no, input_dataset_version)
);

CREATE INDEX IF NOT EXISTS dynamic_model_checkpoints_lookup_idx
    ON dynamic_model_checkpoints (season_id, round_no, created_at DESC);

-- Shadow predictions are never returned by the normal prediction endpoints and
-- cannot be mistaken for frozen production runs. They allow a candidate to be
-- evaluated against the same cutoffs before promotion.
CREATE TABLE IF NOT EXISTS prediction_shadow_runs (
    shadow_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    model_version text NOT NULL,
    prediction_as_of timestamptz NOT NULL,
    cutoff_at timestamptz NOT NULL,
    input_dataset_version text NOT NULL,
    status text NOT NULL CHECK (status IN ('generated', 'failed', 'settled')),
    generated_fixture_count smallint NOT NULL DEFAULT 0 CHECK (generated_fixture_count BETWEEN 0 AND 8),
    failure_reason text,
    manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (season_id, round_no, model_definition_id, model_version, input_dataset_version)
);

CREATE TABLE IF NOT EXISTS prediction_shadow_revisions (
    shadow_revision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shadow_run_id uuid NOT NULL REFERENCES prediction_shadow_runs (shadow_run_id) ON DELETE CASCADE,
    match_id bigint NOT NULL REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    home_probability numeric(18, 12) NOT NULL CHECK (home_probability BETWEEN 0 AND 1),
    draw_probability numeric(18, 12) NOT NULL CHECK (draw_probability BETWEEN 0 AND 1),
    away_probability numeric(18, 12) NOT NULL CHECK (away_probability BETWEEN 0 AND 1),
    probability_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (abs((home_probability + draw_probability + away_probability) - 1) < 0.000001),
    UNIQUE (shadow_run_id, match_id)
);

CREATE INDEX IF NOT EXISTS prediction_shadow_lookup_idx
    ON prediction_shadow_runs (season_id, round_no, created_at DESC);

-- The singleton selection is changed only by the explicit promotion command.
-- The worker continues to use PREDICTION_MODEL until an operator chooses to
-- activate a registry entry, which makes deployment and rollback explicit.
CREATE TABLE IF NOT EXISTS production_model_selection (
    selection_key text PRIMARY KEY CHECK (selection_key = 'production'),
    model_artifact_id uuid NOT NULL REFERENCES model_artifacts (model_artifact_id) ON DELETE RESTRICT,
    model_key text NOT NULL,
    model_version text NOT NULL,
    selected_at timestamptz NOT NULL DEFAULT now(),
    selected_by text NOT NULL,
    reason text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

INSERT INTO schema_migrations (version)
VALUES ('012_phase6_dynamic_bayesian')
ON CONFLICT (version) DO NOTHING;

COMMIT;
