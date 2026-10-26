package research

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ModelArtifactRegistration struct {
	ModelKey             string
	Version              string
	Description          string
	Algorithm            string
	ArtifactPath         string
	ArtifactSHA256       string
	FeatureSchemaVersion string
	TrainingCutoff       time.Time
	DatasetFingerprint   string
	Hyperparameters      any
	CalibrationConfig    any
	EvaluationMetrics    any
	RandomSeed           *int64
}

type ModelExperimentRegistration struct {
	ExperimentKey      string
	DatasetFingerprint string
	CandidateKey       string
	Algorithm          string
	FoldConfiguration  any
	Hyperparameters    any
	Metrics            any
	RandomSeed         *int64
	Status             string
	StartedAt          time.Time
	CompletedAt        *time.Time
	CodeVersion        string
}

// SaveDynamicCheckpoint records the deterministic state used by a Phase 6
// prediction run. It is safe to call repeatedly for the same input fingerprint
// and never overwrites an existing checkpoint.
func (s *ResearchStorage) SaveDynamicCheckpoint(ctx context.Context, modelDefinitionID int64, seasonID string, round int, cutoff time.Time, inputVersion string, model *DynamicBayesianModel, provenance any) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("research storage database is nil")
	}
	if modelDefinitionID <= 0 || strings.TrimSpace(seasonID) == "" || round < 1 || round > 30 || cutoff.IsZero() || strings.TrimSpace(inputVersion) == "" || model == nil {
		return "", fmt.Errorf("invalid dynamic model checkpoint input")
	}
	state, err := jsonValue(map[string]any{
		"parameters":       model.Parameters(),
		"league_intercept": model.LeagueIntercept,
		"home_advantage":   model.HomeAdvantage,
		"current_season":   model.CurrentSeason,
		"last_match_at":    model.LastMatchAt,
		"total_matches":    model.TotalMatches,
		"season_matches":   model.SeasonMatches,
		"states":           model.StateSnapshot(),
	})
	if err != nil {
		return "", err
	}
	provenanceJSON, err := jsonValue(provenance)
	if err != nil {
		return "", err
	}
	var checkpointID string
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO dynamic_model_checkpoints (
    model_definition_id, season_id, round_no, cutoff_at, input_dataset_version,
    state, provenance
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb)
ON CONFLICT (model_definition_id, season_id, round_no, input_dataset_version) DO NOTHING
RETURNING checkpoint_id::text`, modelDefinitionID, seasonID, round, cutoff.UTC(), inputVersion, state, provenanceJSON).Scan(&checkpointID)
	if err == sql.ErrNoRows {
		err = s.DB.QueryRowContext(ctx, `
SELECT checkpoint_id::text
FROM dynamic_model_checkpoints
WHERE model_definition_id = $1 AND season_id = $2 AND round_no = $3 AND input_dataset_version = $4`, modelDefinitionID, seasonID, round, inputVersion).Scan(&checkpointID)
	}
	if err != nil {
		return "", fmt.Errorf("save dynamic model checkpoint: %w", err)
	}
	return checkpointID, nil
}

// SelectProductionArtifact is the explicit Phase 6 promotion gate. Only an
// approved or already-active artifact can become the singleton production
// selection; the previous selection is retired atomically.
func (s *ResearchStorage) SelectProductionArtifact(ctx context.Context, artifactID, selectedBy, reason string, metadata any) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("research storage database is nil")
	}
	if strings.TrimSpace(artifactID) == "" || strings.TrimSpace(selectedBy) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("artifact, selected_by, and reason are required")
	}
	metadataJSON, err := jsonValue(metadata)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin production model selection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var modelKey, modelVersion, promotionStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT m.model_key, m.version, a.promotion_status
FROM model_artifacts a
JOIN model_definitions m ON m.model_definition_id = a.model_definition_id
WHERE a.model_artifact_id = $1::uuid
FOR UPDATE`, artifactID).Scan(&modelKey, &modelVersion, &promotionStatus); err != nil {
		return fmt.Errorf("load production candidate: %w", err)
	}
	if promotionStatus != "approved" && promotionStatus != "active" {
		return fmt.Errorf("artifact must be approved before production selection; current status=%s", promotionStatus)
	}
	if promotionStatus == "approved" {
		if _, err := tx.ExecContext(ctx, `
UPDATE model_artifacts
SET promotion_status = 'active', approved_at = COALESCE(approved_at, now())
WHERE model_artifact_id = $1::uuid`, artifactID); err != nil {
			return fmt.Errorf("activate production artifact: %w", err)
		}
	}
	var previous string
	if err := tx.QueryRowContext(ctx, `SELECT model_artifact_id::text FROM production_model_selection WHERE selection_key = 'production' FOR UPDATE`).Scan(&previous); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("load previous production selection: %w", err)
	}
	if previous != "" && previous != artifactID {
		if _, err := tx.ExecContext(ctx, `UPDATE model_artifacts SET promotion_status = 'retired', retired_at = COALESCE(retired_at, now()) WHERE model_artifact_id = $1::uuid AND promotion_status = 'active'`, previous); err != nil {
			return fmt.Errorf("retire previous production artifact: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO production_model_selection (selection_key, model_artifact_id, model_key, model_version, selected_at, selected_by, reason, metadata)
VALUES ('production', $1::uuid, $2, $3, $4, $5, $6, $7::jsonb)
ON CONFLICT (selection_key) DO UPDATE SET
    model_artifact_id = EXCLUDED.model_artifact_id,
    model_key = EXCLUDED.model_key,
    model_version = EXCLUDED.model_version,
    selected_at = EXCLUDED.selected_at,
    selected_by = EXCLUDED.selected_by,
    reason = EXCLUDED.reason,
    metadata = EXCLUDED.metadata`, artifactID, modelKey, modelVersion, s.now(), selectedBy, reason, metadataJSON); err != nil {
		return fmt.Errorf("write production model selection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit production model selection: %w", err)
	}
	return nil
}

func (s *ResearchStorage) RegisterModelArtifact(ctx context.Context, registration ModelArtifactRegistration) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("research storage database is nil")
	}
	if err := validateArtifactRegistration(registration); err != nil {
		return "", err
	}
	parameters, err := jsonValue(registration.Hyperparameters)
	if err != nil {
		return "", err
	}
	metrics, err := jsonValue(registration.EvaluationMetrics)
	if err != nil {
		return "", err
	}
	calibration, err := jsonValue(registration.CalibrationConfig)
	if err != nil {
		return "", err
	}
	var modelDefinitionID int64
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO model_definitions (model_key, version, description, parameters)
VALUES ($1, $2, $3, $4::jsonb)
ON CONFLICT (model_key, version) DO UPDATE SET
    description = EXCLUDED.description,
    parameters = EXCLUDED.parameters
RETURNING model_definition_id`, registration.ModelKey, registration.Version,
		registration.Description, parameters).Scan(&modelDefinitionID)
	if err != nil {
		return "", fmt.Errorf("ensure model definition for artifact: %w", err)
	}
	var artifactID string
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO model_artifacts (
    model_definition_id, algorithm, artifact_path, artifact_sha256,
    feature_schema_version, training_cutoff, dataset_fingerprint,
    hyperparameters, calibration_config, evaluation_metrics, random_seed,
    promotion_status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11, 'candidate')
ON CONFLICT (model_definition_id, artifact_sha256) DO NOTHING
RETURNING model_artifact_id::text`, modelDefinitionID, registration.Algorithm,
		registration.ArtifactPath, strings.ToLower(registration.ArtifactSHA256),
		registration.FeatureSchemaVersion, registration.TrainingCutoff.UTC(),
		strings.ToLower(registration.DatasetFingerprint), parameters, calibration,
		metrics, registration.RandomSeed).Scan(&artifactID)
	if err == sql.ErrNoRows {
		err = s.DB.QueryRowContext(ctx, `
SELECT model_artifact_id::text
FROM model_artifacts
WHERE model_definition_id = $1 AND artifact_sha256 = $2`, modelDefinitionID,
			strings.ToLower(registration.ArtifactSHA256)).Scan(&artifactID)
	}
	if err != nil {
		return "", fmt.Errorf("register model artifact: %w", err)
	}
	return artifactID, nil
}

func (s *ResearchStorage) RecordModelExperiment(ctx context.Context, registration ModelExperimentRegistration) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("research storage database is nil")
	}
	if strings.TrimSpace(registration.ExperimentKey) == "" || strings.TrimSpace(registration.DatasetFingerprint) == "" || strings.TrimSpace(registration.CandidateKey) == "" {
		return fmt.Errorf("experiment key, candidate key, and dataset fingerprint are required")
	}
	if registration.StartedAt.IsZero() {
		return fmt.Errorf("experiment start time is required")
	}
	if registration.Status != "running" && registration.Status != "completed" && registration.Status != "failed" {
		return fmt.Errorf("invalid experiment status %q", registration.Status)
	}
	folds, err := jsonValue(registration.FoldConfiguration)
	if err != nil {
		return err
	}
	parameters, err := jsonValue(registration.Hyperparameters)
	if err != nil {
		return err
	}
	metrics, err := jsonValue(registration.Metrics)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
INSERT INTO model_experiments (
    experiment_key, dataset_fingerprint, candidate_key, algorithm,
    fold_configuration, hyperparameters, metrics, random_seed, status,
    started_at, completed_at, code_version
) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11, NULLIF($12, ''))
ON CONFLICT (experiment_key) DO UPDATE SET
    fold_configuration = EXCLUDED.fold_configuration,
    hyperparameters = EXCLUDED.hyperparameters,
    metrics = EXCLUDED.metrics,
    status = EXCLUDED.status,
    completed_at = EXCLUDED.completed_at,
    code_version = EXCLUDED.code_version`, registration.ExperimentKey,
		strings.ToLower(registration.DatasetFingerprint), registration.CandidateKey,
		registration.Algorithm, folds, parameters, metrics, registration.RandomSeed,
		registration.Status, registration.StartedAt.UTC(), nullableTimePointer(registration.CompletedAt),
		registration.CodeVersion)
	if err != nil {
		return fmt.Errorf("record model experiment: %w", err)
	}
	return nil
}

func (s *ResearchStorage) SetModelPromotionStatus(ctx context.Context, artifactID, status string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("research storage database is nil")
	}
	if status != "candidate" && status != "approved" && status != "active" && status != "retired" {
		return fmt.Errorf("invalid model promotion status %q", status)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin promotion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT promotion_status FROM model_artifacts WHERE model_artifact_id = $1::uuid FOR UPDATE`, artifactID).Scan(&current); err != nil {
		return fmt.Errorf("load model artifact status: %w", err)
	}
	if !validPromotionTransition(current, status) {
		return fmt.Errorf("invalid model promotion transition %s -> %s", current, status)
	}
	now := s.now()
	var approvedAt, retiredAt any
	if status == "approved" {
		approvedAt = now
	}
	if status == "retired" {
		retiredAt = now
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE model_artifacts
SET promotion_status = $2, approved_at = COALESCE($3, approved_at), retired_at = COALESCE($4, retired_at)
WHERE model_artifact_id = $1::uuid`, artifactID, status, approvedAt, retiredAt); err != nil {
		return fmt.Errorf("update model promotion status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit model promotion status: %w", err)
	}
	return nil
}

// ApproveModelArtifact records the human approval metadata for a Phase 6
// candidate. It deliberately accepts only the dynamic Bayesian candidate and
// never selects it for production; SelectProductionArtifact remains a
// separate explicit action.
func (s *ResearchStorage) ApproveModelArtifact(ctx context.Context, artifactID, approvedBy, reason string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("research storage database is nil")
	}
	if strings.TrimSpace(artifactID) == "" || strings.TrimSpace(approvedBy) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("artifact, approved_by, and reason are required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin artifact approval: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var modelKey, version, algorithm, status string
	if err := tx.QueryRowContext(ctx, `
SELECT m.model_key, m.version, a.algorithm, a.promotion_status
FROM model_artifacts a
JOIN model_definitions m ON m.model_definition_id = a.model_definition_id
WHERE a.model_artifact_id = $1::uuid
FOR UPDATE`, artifactID).Scan(&modelKey, &version, &algorithm, &status); err != nil {
		return fmt.Errorf("load approval candidate: %w", err)
	}
	if modelKey != DynamicBayesianModelName || version != DynamicBayesianModelVersion || algorithm != "dynamic_bayesian" {
		return fmt.Errorf("artifact is not the Phase 6 dynamic Bayesian candidate")
	}
	if status != "candidate" {
		return fmt.Errorf("artifact must be candidate before approval; current status=%s", status)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE model_artifacts
SET promotion_status = 'approved', approved_at = now(), approved_by = $2, approval_reason = $3
WHERE model_artifact_id = $1::uuid`, artifactID, approvedBy, reason); err != nil {
		return fmt.Errorf("approve model artifact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit artifact approval: %w", err)
	}
	return nil
}

func validateArtifactRegistration(registration ModelArtifactRegistration) error {
	for label, value := range map[string]string{
		"model key":              registration.ModelKey,
		"version":                registration.Version,
		"algorithm":              registration.Algorithm,
		"artifact path":          registration.ArtifactPath,
		"artifact checksum":      registration.ArtifactSHA256,
		"feature schema version": registration.FeatureSchemaVersion,
		"dataset fingerprint":    registration.DatasetFingerprint,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
	}
	if registration.Algorithm != "baseline" && registration.Algorithm != "lightgbm" && registration.Algorithm != "dynamic_bayesian" {
		return fmt.Errorf("invalid artifact algorithm %q", registration.Algorithm)
	}
	if len(registration.ArtifactSHA256) != 64 || len(registration.DatasetFingerprint) != 64 {
		return fmt.Errorf("artifact checksum and dataset fingerprint must be SHA-256 values")
	}
	if registration.TrainingCutoff.IsZero() {
		return fmt.Errorf("training cutoff is required")
	}
	return nil
}

func validPromotionTransition(current, next string) bool {
	if current == next {
		return true
	}
	switch current {
	case "candidate":
		return next == "approved" || next == "retired"
	case "approved":
		return next == "active" || next == "retired"
	case "active":
		return next == "retired"
	default:
		return false
	}
}

func nullableTimePointer(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}
