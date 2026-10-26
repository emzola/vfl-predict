package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/research"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "SQL migrations directory")
	artifactPath := flag.String("artifact-path", "", "immutable deployed API image reference")
	artifactSHA256 := flag.String("artifact-sha256", "", "immutable deployed API image digest without sha256: prefix")
	selectedBy := flag.String("selected-by", "github-actions-direct", "identity recorded in the production selection audit row")
	reason := flag.String("reason", "Direct Phase 6 activation without replay or shadow evaluation", "activation reason")
	confirm := flag.String("confirm", "", "must be YES to activate without replay")
	flag.Parse()

	if *databaseURL == "" || strings.TrimSpace(*artifactPath) == "" || !sha256Pattern.MatchString(*artifactSHA256) {
		fatal("database URL, artifact path, and a 64-character artifact SHA-256 are required")
	}
	if *confirm != "YES" {
		fatal("set -confirm YES to authorize direct activation without replay")
	}
	if strings.TrimSpace(*selectedBy) == "" || strings.TrimSpace(*reason) == "" {
		fatal("selected-by and reason are required")
	}
	if !strings.Contains(*artifactPath, "@sha256:"+strings.ToLower(*artifactSHA256)) {
		fatal("artifact path must contain the supplied immutable sha256 digest")
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
	activationFingerprint := sha256.Sum256([]byte("phase6-direct-activation|" + *artifactPath + "|" + strings.ToLower(*artifactSHA256) + "|" + research.DynamicBayesianModelVersion))
	registration := research.ModelArtifactRegistration{
		ModelKey:             research.DynamicBayesianModelName,
		Version:              research.DynamicBayesianModelVersion,
		Description:          "Unified dynamic team-strength and Bayesian attack/defense scoreline model",
		Algorithm:            "dynamic_bayesian",
		ArtifactPath:         *artifactPath,
		ArtifactSHA256:       strings.ToLower(*artifactSHA256),
		FeatureSchemaVersion: "phase6.v1",
		TrainingCutoff:       time.Now().UTC(),
		DatasetFingerprint:   hex.EncodeToString(activationFingerprint[:]),
		Hyperparameters: map[string]any{
			"promotion_mode":    "direct_no_replay",
			"replay":            "not_run",
			"shadow_evaluation": "not_run",
			"configuration":     research.DefaultDynamicBayesianConfig(),
		},
		CalibrationConfig: map[string]any{
			"method": "none",
			"status": "not_fitted",
		},
		EvaluationMetrics: map[string]any{
			"promotion_mode":    "direct_no_replay",
			"historical_replay": "not_run",
			"shadow_evaluation": "not_run",
			"reason":            *reason,
		},
	}
	artifactID, err := storage.RegisterModelArtifact(ctx, registration)
	if err != nil {
		fatal(err.Error())
	}

	status, err := artifactStatus(ctx, db, artifactID)
	if err != nil {
		fatal(err.Error())
	}
	if status == "retired" {
		fatal("the immutable image artifact is retired; deploy a new release digest")
	}
	if status == "candidate" {
		if err := storage.ApproveModelArtifact(ctx, artifactID, *selectedBy, "Direct activation authorized without replay or shadow evaluation"); err != nil {
			fatal(err.Error())
		}
	}
	if err := storage.SelectProductionArtifact(ctx, artifactID, *selectedBy, *reason, map[string]any{
		"phase":              6,
		"promotion_mode":     "direct_no_replay",
		"historical_replay":  "not_run",
		"shadow_evaluation":  "not_run",
		"workflow_automated": true,
	}); err != nil {
		fatal(err.Error())
	}

	fmt.Printf("directly activated Phase 6 model %s %s artifact=%s without replay or shadow evaluation\n", research.DynamicBayesianModelName, research.DynamicBayesianModelVersion, artifactID)
}

func artifactStatus(ctx context.Context, db *sql.DB, artifactID string) (string, error) {
	var status string
	err := db.QueryRowContext(ctx, `SELECT promotion_status FROM model_artifacts WHERE model_artifact_id = $1::uuid`, artifactID).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("load direct activation artifact status: %w", err)
	}
	return status, nil
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "phase6-activate:", message)
	os.Exit(1)
}
