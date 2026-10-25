package research

import (
	"context"
	"os"
	"testing"

	"bet9ja-vfl/services/ingest"
)

func TestPhase2DatabaseSchemaAndResearchStorage(t *testing.T) {
	databaseURL := os.Getenv("PHASE2_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PHASE1_DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set PHASE2_DATABASE_URL or PHASE1_DATABASE_URL to a dedicated PostgreSQL database")
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
	for _, table := range []string{
		"feature_definitions", "feature_snapshots", "model_definitions", "training_runs",
		"backtest_runs", "research_predictions", "evaluation_metrics",
		"betting_analysis_runs", "betting_analysis_selections", "betting_analysis_metrics",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = $1
)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("phase 2 table %s is missing", table)
		}
	}
	storage := NewResearchStorage(db)
	definitionID, err := storage.EnsureFeatureDefinition(ctx, "integration_test_feature", "test.v1", "integration test", "verified-only", map[string]any{"test": true})
	if err != nil {
		t.Fatal(err)
	}
	if definitionID <= 0 {
		t.Fatalf("feature definition ID=%d", definitionID)
	}
	modelID, err := storage.EnsureModelDefinition(ctx, "integration_test_model", "test.v1", "integration test", map[string]any{"seed": 1})
	if err != nil {
		t.Fatal(err)
	}
	if modelID <= 0 {
		t.Fatalf("model definition ID=%d", modelID)
	}
}
