package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
)

func TestPhase35ModelRegistryIntegration(t *testing.T) {
	databaseURL := os.Getenv("PHASE35_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PHASE3_DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set PHASE35_DATABASE_URL or PHASE3_DATABASE_URL to a dedicated PostgreSQL database")
	}
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"model_artifacts", "model_experiments"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = $1
)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("phase 3.5 table %s is missing", table)
		}
	}

	key := fmt.Sprintf("registry-integration-%d", time.Now().UnixNano())
	digest := sha256.Sum256([]byte(key))
	checksum := hex.EncodeToString(digest[:])
	registration := ModelArtifactRegistration{
		ModelKey:             key,
		Version:              "test.v1",
		Description:          "Phase 3.5 registry integration test",
		Algorithm:            "lightgbm",
		ArtifactPath:         "tmp/registry-integration.txt",
		ArtifactSHA256:       checksum,
		FeatureSchemaVersion: "test.features.v1",
		TrainingCutoff:       time.Now().UTC(),
		DatasetFingerprint:   checksum,
		Hyperparameters:      map[string]any{"seed": 1},
		CalibrationConfig:    map[string]any{"temperature": 1.0},
		EvaluationMetrics:    map[string]any{"log_loss": 1.0},
	}
	storage := NewResearchStorage(db)
	first, err := storage.RegisterModelArtifact(ctx, registration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.RegisterModelArtifact(ctx, registration)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("artifact registration was not idempotent: first=%q second=%q", first, second)
	}
	if err := storage.SetModelPromotionStatus(ctx, first, "approved"); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetModelPromotionStatus(ctx, first, "active"); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetModelPromotionStatus(ctx, first, "retired"); err != nil {
		t.Fatal(err)
	}

	started := time.Now().UTC()
	if err := storage.RecordModelExperiment(ctx, ModelExperimentRegistration{
		ExperimentKey:      key + ":experiment",
		DatasetFingerprint: checksum,
		CandidateKey:       key,
		Algorithm:          "lightgbm",
		FoldConfiguration:  map[string]any{"chronological": true},
		Hyperparameters:    map[string]any{"num_leaves": 7},
		Metrics:            map[string]any{"log_loss": 1.0},
		Status:             "completed",
		StartedAt:          started,
		CompletedAt:        &started,
		CodeVersion:        "test",
	}); err != nil {
		t.Fatal(err)
	}
}
