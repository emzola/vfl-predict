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
	if registration.Algorithm != "baseline" && registration.Algorithm != "lightgbm" {
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
