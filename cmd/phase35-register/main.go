package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/research"
)

type manifest struct {
	Artifacts   []artifactManifest   `json:"artifacts"`
	Experiments []experimentManifest `json:"experiments"`
}

type artifactManifest struct {
	ModelKey             string `json:"model_key"`
	Version              string `json:"version"`
	Description          string `json:"description"`
	Algorithm            string `json:"algorithm"`
	ArtifactPath         string `json:"artifact_path"`
	ArtifactSHA256       string `json:"artifact_sha256"`
	FeatureSchemaVersion string `json:"feature_schema_version"`
	TrainingCutoff       string `json:"training_cutoff"`
	DatasetFingerprint   string `json:"dataset_fingerprint"`
	Hyperparameters      any    `json:"hyperparameters"`
	CalibrationConfig    any    `json:"calibration_config"`
	EvaluationMetrics    any    `json:"evaluation_metrics"`
	RandomSeed           *int64 `json:"random_seed"`
}

type experimentManifest struct {
	ExperimentKey      string `json:"experiment_key"`
	DatasetFingerprint string `json:"dataset_fingerprint"`
	CandidateKey       string `json:"candidate_key"`
	Algorithm          string `json:"algorithm"`
	FoldConfiguration  any    `json:"fold_configuration"`
	Hyperparameters    any    `json:"hyperparameters"`
	Metrics            any    `json:"metrics"`
	RandomSeed         *int64 `json:"random_seed"`
	Status             string `json:"status"`
	StartedAt          string `json:"started_at"`
	CompletedAt        string `json:"completed_at"`
	CodeVersion        string `json:"code_version"`
}

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL")
	manifestPath := flag.String("manifest", "tmp/phase35-manifest.json", "Phase 3.5 manifest JSON")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "migration directory")
	flag.Parse()
	if *databaseURL == "" {
		fatal("database URL is required via -database-url or DATABASE_URL")
	}
	contents, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal(err.Error())
	}
	var input manifest
	if err := json.Unmarshal(contents, &input); err != nil {
		fatal(fmt.Sprintf("decode manifest: %v", err))
	}
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, *databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, *migrationsDir); err != nil {
		fatal(err.Error())
	}
	storage := research.NewResearchStorage(db)
	for _, artifact := range input.Artifacts {
		trainingCutoff, err := parseTime(artifact.TrainingCutoff)
		if err != nil {
			fatal(fmt.Sprintf("artifact %s training cutoff: %v", artifact.ModelKey, err))
		}
		artifactID, err := storage.RegisterModelArtifact(ctx, research.ModelArtifactRegistration{
			ModelKey:             artifact.ModelKey,
			Version:              artifact.Version,
			Description:          artifact.Description,
			Algorithm:            artifact.Algorithm,
			ArtifactPath:         artifact.ArtifactPath,
			ArtifactSHA256:       artifact.ArtifactSHA256,
			FeatureSchemaVersion: artifact.FeatureSchemaVersion,
			TrainingCutoff:       trainingCutoff,
			DatasetFingerprint:   artifact.DatasetFingerprint,
			Hyperparameters:      artifact.Hyperparameters,
			CalibrationConfig:    artifact.CalibrationConfig,
			EvaluationMetrics:    artifact.EvaluationMetrics,
			RandomSeed:           artifact.RandomSeed,
		})
		if err != nil {
			fatal(err.Error())
		}
		fmt.Printf("registered candidate artifact %s model=%s version=%s\n", artifactID, artifact.ModelKey, artifact.Version)
	}
	for _, experiment := range input.Experiments {
		startedAt, err := parseTime(experiment.StartedAt)
		if err != nil {
			fatal(fmt.Sprintf("experiment %s start: %v", experiment.ExperimentKey, err))
		}
		var completedAt *time.Time
		if experiment.CompletedAt != "" {
			value, parseErr := parseTime(experiment.CompletedAt)
			if parseErr != nil {
				fatal(fmt.Sprintf("experiment %s completion: %v", experiment.ExperimentKey, parseErr))
			}
			completedAt = &value
		}
		if err := storage.RecordModelExperiment(ctx, research.ModelExperimentRegistration{
			ExperimentKey:      experiment.ExperimentKey,
			DatasetFingerprint: experiment.DatasetFingerprint,
			CandidateKey:       experiment.CandidateKey,
			Algorithm:          experiment.Algorithm,
			FoldConfiguration:  experiment.FoldConfiguration,
			Hyperparameters:    experiment.Hyperparameters,
			Metrics:            experiment.Metrics,
			RandomSeed:         experiment.RandomSeed,
			Status:             experiment.Status,
			StartedAt:          startedAt,
			CompletedAt:        completedAt,
			CodeVersion:        experiment.CodeVersion,
		}); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("recorded experiment %s\n", experiment.ExperimentKey)
	}
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is required")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
