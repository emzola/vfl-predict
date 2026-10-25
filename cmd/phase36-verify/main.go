package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/live"
)

type verification struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func main() {
	databaseURL := os.Getenv("PHASE36_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	reportPath := ""
	flag.StringVar(&databaseURL, "database-url", databaseURL, "optional PostgreSQL connection URL")
	flag.StringVar(&reportPath, "report", reportPath, "optional Phase 3.6 JSON report to validate")
	flag.Parse()

	checks := []verification{
		checkPhase3Files(),
		checkLeaseRecoveryFiles(),
		checkProviderBudget(),
		checkCredentialHygiene(),
	}
	if reportPath != "" {
		checks = append(checks, checkReport(reportPath))
	}
	if databaseURL != "" {
		checks = append(checks, checkDatabase(databaseURL))
	}
	for _, check := range checks {
		fmt.Printf("%-30s %v %s\n", check.Name, check.Passed, check.Detail)
		if !check.Passed {
			os.Exit(1)
		}
	}
	encoded, _ := json.MarshalIndent(map[string]any{"phase": 3.6, "network_requests": 0, "checks": checks}, "", "  ")
	fmt.Println(string(encoded))
}

func checkPhase3Files() verification {
	paths := []string{
		filepath.Join("db", "migrations", "004_phase3_operational.sql"),
		filepath.Join("db", "migrations", "005_phase3_observation_history.sql"),
		filepath.Join("db", "migrations", "006_phase3_observation_constraint_cleanup.sql"),
		filepath.Join("db", "migrations", "007_phase3_odds_observation_history.sql"),
		filepath.Join("db", "migrations", "008_phase35_model_registry.sql"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return verification{"phase3_phase35_migrations", false, err.Error()}
		}
	}
	return verification{"phase3_phase35_migrations", true, "existing Phase 3 and 3.5 migrations are present"}
}

func checkLeaseRecoveryFiles() verification {
	for _, path := range []string{
		filepath.Join("cmd", "phase36-lease", "main.go"),
		filepath.Join("services", "ingest", "lease_integration_test.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			return verification{"lease_recovery_path", false, err.Error()}
		}
	}
	return verification{"lease_recovery_path", true, "inspection and expired-only recovery path are present"}
}

func checkProviderBudget() verification {
	cfg := ingest.DefaultConfig()
	if cfg.RequestSpacing < 10*time.Second {
		return verification{"provider_request_budget", false, fmt.Sprintf("default spacing=%s", cfg.RequestSpacing)}
	}
	plan := live.DefaultPollConfig()
	if plan.UpcomingOddsInterval < time.Second || plan.FutureOddsInterval < plan.UpcomingOddsInterval {
		return verification{"provider_request_budget", false, "adaptive polling intervals are invalid"}
	}
	return verification{"provider_request_budget", true, fmt.Sprintf("shared minimum spacing=%s; adaptive polling remains enabled", cfg.RequestSpacing)}
}

func checkCredentialHygiene() verification {
	for _, name := range []string{"BET9JA_API_KEY", "BET9JA_TOKEN", "BET9JA_PASSWORD"} {
		if value := os.Getenv(name); strings.TrimSpace(value) != "" {
			return verification{"credential_hygiene", false, name + " must not be used by verification"}
		}
	}
	return verification{"credential_hygiene", true, "no provider credentials read"}
}

func checkReport(path string) verification {
	contents, err := os.ReadFile(path)
	if err != nil {
		return verification{"phase36_report", false, err.Error()}
	}
	var document struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return verification{"phase36_report", false, err.Error()}
	}
	if document.Phase != "3.6" {
		return verification{"phase36_report", false, fmt.Sprintf("phase=%q", document.Phase)}
	}
	return verification{"phase36_report", true, "report is valid JSON for Phase 3.6"}
}

func checkDatabase(databaseURL string) verification {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		return verification{"phase36_database", false, err.Error()}
	}
	defer db.Close()
	for _, table := range []string{
		"live_feed_observations", "odds_snapshot_observations", "odds_change_history",
		"round_states", "prediction_runs", "prediction_revisions", "prediction_settlements",
		"forward_paper_results", "scheduler_leases", "provider_request_budget", "job_execution_history",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
			return verification{"phase36_database", false, err.Error()}
		}
		if !exists {
			return verification{"phase36_database", false, "missing table " + table}
		}
	}
	repo := ingest.NewRepository(db)
	lease, err := repo.GetLease(ctx, "live-monitor")
	if err != nil {
		return verification{"phase36_database", false, err.Error()}
	}
	if lease.Exists && lease.Expired {
		return verification{"phase36_database", false, "expired live-monitor lease remains; inspect and reclaim only after confirming no worker owns it"}
	}
	var failed, running int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status = 'failed'), count(*) FILTER (WHERE status = 'running') FROM job_execution_history WHERE job_key = 'live-monitor'`).Scan(&failed, &running); err != nil {
		return verification{"phase36_database", false, err.Error()}
	}
	return verification{"phase36_database", true, fmt.Sprintf("operational tables present; live-monitor lease valid_or_absent=%t failed_jobs=%d running_jobs=%d", !lease.Exists || !lease.Expired, failed, running)}
}
