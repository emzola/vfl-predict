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
	for _, table := range []string{"model_artifacts", "model_experiments", "dynamic_model_checkpoints", "prediction_shadow_runs", "prediction_shadow_revisions", "production_model_selection"} {
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

func TestProductionSelectionRetiresPreviousActiveArtifact(t *testing.T) {
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

	key := fmt.Sprintf("selection-transition-%d", time.Now().UnixNano())
	storage := NewResearchStorage(db)
	makeRegistration := func(suffix string) ModelArtifactRegistration {
		digest := sha256.Sum256([]byte(key + suffix))
		checksum := hex.EncodeToString(digest[:])
		return ModelArtifactRegistration{
			ModelKey:             key,
			Version:              "test.v1",
			Description:          "production selection transition integration test",
			Algorithm:            "lightgbm",
			ArtifactPath:         "tmp/selection-transition-" + suffix,
			ArtifactSHA256:       checksum,
			FeatureSchemaVersion: "test.features.v1",
			TrainingCutoff:       time.Now().UTC(),
			DatasetFingerprint:   checksum,
			Hyperparameters:      map[string]any{"seed": 1},
			CalibrationConfig:    map[string]any{},
			EvaluationMetrics:    map[string]any{},
		}
	}

	first, err := storage.RegisterModelArtifact(ctx, makeRegistration("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.RegisterModelArtifact(ctx, makeRegistration("second"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected distinct immutable artifacts")
	}
	for _, artifactID := range []string{first, second} {
		if err := storage.SetModelPromotionStatus(ctx, artifactID, "approved"); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.SelectProductionArtifact(ctx, first, "integration-test", "select first artifact", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := storage.SelectProductionArtifact(ctx, second, "integration-test", "replace first artifact", map[string]any{}); err != nil {
		t.Fatalf("replacement selection should retire the previous active artifact before activation: %v", err)
	}
	if err := storage.SelectProductionArtifact(ctx, second, "integration-test", "repeat second selection", map[string]any{}); err != nil {
		t.Fatalf("repeating an already-active selection should be idempotent: %v", err)
	}

	var firstStatus, secondStatus string
	if err := db.QueryRowContext(ctx, `SELECT promotion_status FROM model_artifacts WHERE model_artifact_id = $1::uuid`, first).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT promotion_status FROM model_artifacts WHERE model_artifact_id = $1::uuid`, second).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "retired" || secondStatus != "active" {
		t.Fatalf("unexpected transition statuses: first=%s second=%s", firstStatus, secondStatus)
	}
}
