BEGIN;

-- Phase 3.5 keeps model binaries and their promotion state separate from the
-- stable model definition. An artifact is immutable; a new checksum requires
-- a new row rather than overwriting an earlier research result.
CREATE TABLE IF NOT EXISTS model_artifacts (
    model_artifact_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_definition_id bigint NOT NULL REFERENCES model_definitions (model_definition_id) ON DELETE RESTRICT,
    algorithm text NOT NULL CHECK (algorithm IN ('baseline', 'lightgbm')),
    artifact_path text NOT NULL,
    artifact_sha256 char(64) NOT NULL CHECK (artifact_sha256 ~ '^[0-9a-f]{64}$'),
    feature_schema_version text NOT NULL,
    training_cutoff timestamptz NOT NULL,
    dataset_fingerprint char(64) NOT NULL CHECK (dataset_fingerprint ~ '^[0-9a-f]{64}$'),
    hyperparameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    calibration_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    evaluation_metrics jsonb NOT NULL DEFAULT '{}'::jsonb,
    random_seed bigint,
    promotion_status text NOT NULL DEFAULT 'candidate'
        CHECK (promotion_status IN ('candidate', 'approved', 'active', 'retired')),
    created_at timestamptz NOT NULL DEFAULT now(),
    approved_at timestamptz,
    retired_at timestamptz,
    UNIQUE (model_definition_id, artifact_sha256),
    CHECK (promotion_status <> 'approved' OR approved_at IS NOT NULL),
    CHECK (promotion_status <> 'retired' OR retired_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS model_artifacts_lookup_idx
    ON model_artifacts (model_definition_id, promotion_status, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS model_artifacts_one_active_per_key_idx
    ON model_artifacts (model_definition_id)
    WHERE promotion_status = 'active';

CREATE TABLE IF NOT EXISTS model_experiments (
    model_experiment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_key text NOT NULL UNIQUE,
    dataset_fingerprint char(64) NOT NULL CHECK (dataset_fingerprint ~ '^[0-9a-f]{64}$'),
    candidate_key text NOT NULL,
    algorithm text NOT NULL CHECK (algorithm IN ('lightgbm', 'baseline', 'calibration', 'ensemble')),
    fold_configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    hyperparameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    metrics jsonb NOT NULL DEFAULT '{}'::jsonb,
    random_seed bigint,
    status text NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    code_version text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);

CREATE INDEX IF NOT EXISTS model_experiments_dataset_idx
    ON model_experiments (dataset_fingerprint, candidate_key, created_at DESC);

INSERT INTO schema_migrations (version)
VALUES ('008_phase35_model_registry')
ON CONFLICT (version) DO NOTHING;

COMMIT;
