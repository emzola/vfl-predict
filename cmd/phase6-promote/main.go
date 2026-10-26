package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/research"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "SQL migrations directory")
	artifactID := flag.String("artifact-id", "", "approved model_artifact_id to select")
	selectedBy := flag.String("selected-by", "operator", "operator identity recorded in the audit row")
	reason := flag.String("reason", "Phase 6 acceptance gates passed", "promotion reason")
	flag.Parse()
	if *databaseURL == "" || *artifactID == "" {
		fatal("database URL and -artifact-id are required")
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
	var modelKey, version, algorithm, status string
	err = db.QueryRowContext(ctx, `
SELECT m.model_key, m.version, a.algorithm, a.promotion_status
FROM model_artifacts a
JOIN model_definitions m ON m.model_definition_id = a.model_definition_id
WHERE a.model_artifact_id = $1::uuid`, *artifactID).Scan(&modelKey, &version, &algorithm, &status)
	if err == sql.ErrNoRows {
		fatal("model artifact was not found")
	}
	if err != nil {
		fatal(fmt.Sprintf("load model artifact: %v", err))
	}
	if modelKey != research.DynamicBayesianModelName || version != research.DynamicBayesianModelVersion || algorithm != "dynamic_bayesian" {
		fatal(fmt.Sprintf("artifact is not the Phase 6 candidate: model=%s version=%s algorithm=%s", modelKey, version, algorithm))
	}
	if status != "approved" && status != "active" {
		fatal(fmt.Sprintf("artifact must be approved before selection; current status=%s", status))
	}
	if err := research.NewResearchStorage(db).SelectProductionArtifact(ctx, *artifactID, *selectedBy, *reason, map[string]any{"phase": 6, "explicit_operator_action": true}); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("selected production artifact %s\n", *artifactID)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "phase6-promote:", message)
	os.Exit(1)
}
