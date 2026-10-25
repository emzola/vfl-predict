package storageaudit

import (
	"context"
	"os"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
)

func TestStorageAuditAndRetentionPlanIntegration(t *testing.T) {
	databaseURL := os.Getenv("PHASE42_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set PHASE42_DATABASE_URL to a dedicated PostgreSQL database")
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
	report, err := CollectDatabase(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if report.DatabaseName == "" || report.DatabaseBytes < 0 {
		t.Fatalf("invalid database report: %+v", report)
	}
	plan, err := LoadRetentionPlan(ctx, db, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !plan.DryRun || len(plan.Policies) == 0 {
		t.Fatalf("invalid retention plan: %+v", plan)
	}
	required := map[string]bool{
		"fixtures_results_and_teams":      false,
		"verified_pre_cutoff_odds":        false,
		"frozen_predictions_and_settlements": false,
		"raw_provider_payloads":           false,
		"job_execution_history":           false,
		"auth_ephemeral_tokens_and_rate_limits": false,
	}
	for _, policy := range plan.Policies {
		if _, ok := required[policy.PolicyKey]; ok {
			required[policy.PolicyKey] = true
		}
	}
	for key, found := range required {
		if !found {
			t.Fatalf("missing required retention policy %q", key)
		}
	}
}
