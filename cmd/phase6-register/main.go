package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/research"
)

type replayReport struct {
	SchemaVersion        string          `json:"schema_version"`
	GeneratedAt          time.Time       `json:"generated_at"`
	DatasetFingerprint   string          `json:"dataset_fingerprint"`
	EvaluatedSeasonCount int             `json:"evaluated_season_count"`
	FinalMatchCount      int             `json:"final_match_count"`
	DynamicReplay        json.RawMessage `json:"dynamic_replay"`
}

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "SQL migrations directory")
	reportPath := flag.String("report", "", "Phase 6 replay report JSON")
	artifactPath := flag.String("artifact-path", "builtin://dynamic_bayesian_team_strength/phase6.v1", "immutable code/image artifact reference")
	artifactSHA := flag.String("artifact-sha256", os.Getenv("PHASE6_ARTIFACT_SHA256"), "SHA-256 of the immutable deployed artifact")
	featureSchema := flag.String("feature-schema-version", "phase6.scoreline.v1", "feature/schema version recorded with the artifact")
	modelKey := flag.String("model-key", research.DynamicBayesianModelName, "model registry key")
	modelVersion := flag.String("model-version", research.DynamicBayesianModelVersion, "model registry version")
	flag.Parse()

	if strings.TrimSpace(*databaseURL) == "" || strings.TrimSpace(*reportPath) == "" || strings.TrimSpace(*artifactSHA) == "" {
		fatal("database URL, -report, and -artifact-sha256 (or PHASE6_ARTIFACT_SHA256) are required")
	}
	if *modelKey != research.DynamicBayesianModelName || *modelVersion != research.DynamicBayesianModelVersion {
		fatal("phase6-register only registers dynamic_bayesian_team_strength phase6.v1")
	}
	reportBytes, err := os.ReadFile(*reportPath)
	if err != nil {
		fatal(fmt.Sprintf("read report: %v", err))
	}
	var report replayReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		fatal(fmt.Sprintf("decode report: %v", err))
	}
	if report.SchemaVersion != "phase6.replay.v1" || len(report.DynamicReplay) == 0 {
		fatal("report is not a complete Phase 6 replay report")
	}
	if report.EvaluatedSeasonCount < 1 || report.FinalMatchCount < 1 {
		fatal("report contains no held-out seasons or completed matches")
	}
	if len(report.DatasetFingerprint) != 64 || !isLowerHex(report.DatasetFingerprint) {
		fatal("report dataset fingerprint must be a SHA-256 value")
	}
	artifactDigest := strings.ToLower(strings.TrimSpace(*artifactSHA))
	if len(artifactDigest) != sha256.Size*2 || !isLowerHex(artifactDigest) {
		fatal("artifact SHA-256 must be 64 hexadecimal characters")
	}
	if report.GeneratedAt.IsZero() {
		fatal("report generated_at is required")
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
	artifactID, err := storage.RegisterModelArtifact(ctx, research.ModelArtifactRegistration{
		ModelKey:             *modelKey,
		Version:              *modelVersion,
		Description:          "Phase 6 unified dynamic opponent-adjusted rating and regularized Bayesian attack/defense scoreline model",
		Algorithm:            "dynamic_bayesian",
		ArtifactPath:         *artifactPath,
		ArtifactSHA256:       artifactDigest,
		FeatureSchemaVersion: *featureSchema,
		TrainingCutoff:       report.GeneratedAt,
		DatasetFingerprint:   strings.ToLower(report.DatasetFingerprint),
		Hyperparameters: map[string]any{
			"phase":                        6,
			"model":                        research.DynamicBayesianModelName,
			"model_version":                research.DynamicBayesianModelVersion,
			"research_only_until_approval": true,
		},
		CalibrationConfig: map[string]any{"method": "none"},
		EvaluationMetrics: json.RawMessage(report.DynamicReplay),
	})
	if err != nil {
		fatal(err.Error())
	}
	completedAt := time.Now().UTC()
	if err := storage.RecordModelExperiment(ctx, research.ModelExperimentRegistration{
		ExperimentKey:      "phase6-replay:" + strings.ToLower(report.DatasetFingerprint),
		DatasetFingerprint: strings.ToLower(report.DatasetFingerprint),
		CandidateKey:       research.DynamicBayesianModelName,
		Algorithm:          "dynamic_bayesian",
		FoldConfiguration:  map[string]any{"chronological": true, "season_block_bootstrap": true},
		Hyperparameters:    map[string]any{"model_version": research.DynamicBayesianModelVersion},
		Metrics:            json.RawMessage(report.DynamicReplay),
		Status:             "completed",
		StartedAt:          report.GeneratedAt,
		CompletedAt:        &completedAt,
		CodeVersion:        research.DynamicBayesianModelVersion,
	}); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("registered Phase 6 candidate artifact %s\n", artifactID)
	fmt.Println("status=candidate; approval and production selection remain separate explicit actions")
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "phase6-register:", message)
	os.Exit(1)
}
