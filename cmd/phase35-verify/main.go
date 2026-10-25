package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bet9ja-vfl/services/ingest"
)

type verification struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type reportDocument struct {
	SchemaVersion      string                 `json:"schema_version"`
	OddsPolicy         string                 `json:"odds_policy"`
	SeasonCount        int                    `json:"season_count"`
	CommonEligibleRows int                    `json:"common_eligible_row_count"`
	FinalTestRowCount  int                    `json:"final_test_row_count"`
	FinalTestSeasons   []string               `json:"final_test_seasons"`
	PromotionGates     map[string]gate        `json:"promotion_gates"`
	Models             map[string]modelResult `json:"models"`
	DatasetFingerprint string                 `json:"dataset_fingerprint"`
}

type gate struct {
	EligibleForApproval bool   `json:"eligible_for_approval"`
	PromotionStatus     string `json:"promotion_status"`
}

type modelResult struct {
	Raw struct {
		Metrics map[string]float64 `json:"metrics"`
	} `json:"raw"`
}

func main() {
	reportPath := flag.String("report", "tmp/phase35-lgbm-report.json", "Phase 3.5 report")
	manifestPath := flag.String("manifest", "tmp/phase35-manifest.json", "Phase 3.5 artifact manifest")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "migration directory")
	databaseURL := flag.String("database-url", os.Getenv("PHASE35_DATABASE_URL"), "optional PostgreSQL URL")
	flag.Parse()

	checks := []verification{
		checkMigration(*migrationsDir),
		checkReport(*reportPath),
		checkManifest(*manifestPath),
		checkCredentialHygiene(),
	}
	if *databaseURL != "" {
		report, err := loadReport(*reportPath)
		if err != nil {
			checks = append(checks, verification{"database_registry", false, err.Error()})
		} else {
			checks = append(checks, checkDatabase(*databaseURL, *migrationsDir, report.DatasetFingerprint))
		}
	} else {
		checks = append(checks, verification{"database_registry", true, "database check skipped; set -database-url for registry verification"})
	}
	for _, check := range checks {
		fmt.Printf("%-28s %v %s\n", check.Name, check.Passed, check.Detail)
		if !check.Passed {
			os.Exit(1)
		}
	}
	encoded, _ := json.MarshalIndent(map[string]any{"phase": 3.5, "network_requests": 0, "checks": checks}, "", "  ")
	fmt.Println(string(encoded))
}

func checkMigration(directory string) verification {
	path := filepath.Join(directory, "008_phase35_model_registry.sql")
	if _, err := os.Stat(path); err != nil {
		return verification{"phase35_migration", false, err.Error()}
	}
	return verification{"phase35_migration", true, "migration 008 model registry present"}
}

func checkReport(path string) verification {
	report, err := loadReport(path)
	if err != nil {
		return verification{"phase35_report", false, err.Error()}
	}
	if report.SchemaVersion != "phase35.report.v1" {
		return verification{"phase35_report", false, "unsupported report schema"}
	}
	if report.SeasonCount < 1 || report.CommonEligibleRows < 1 || report.FinalTestRowCount < 1 || len(report.FinalTestSeasons) < 1 {
		return verification{"phase35_report", false, "report has no eligible final test block"}
	}
	if len(report.DatasetFingerprint) != 64 {
		return verification{"phase35_report", false, "report dataset fingerprint is not SHA-256 shaped"}
	}
	for name, gate := range report.PromotionGates {
		if gate.PromotionStatus != "candidate" || gate.EligibleForApproval {
			return verification{"phase35_report", false, fmt.Sprintf("model %s is not held at candidate status", name)}
		}
	}
	if len(report.Models) < 7 {
		return verification{"phase35_report", false, "required model comparison is incomplete"}
	}
	return verification{"phase35_report", true, fmt.Sprintf("%d seasons, %d eligible rows, %d final-test rows, odds=%s", report.SeasonCount, report.CommonEligibleRows, report.FinalTestRowCount, report.OddsPolicy)}
}

func loadReport(path string) (reportDocument, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return reportDocument{}, err
	}
	var report reportDocument
	if err := json.Unmarshal(contents, &report); err != nil {
		return reportDocument{}, err
	}
	return report, nil
}

func checkManifest(path string) verification {
	contents, err := os.ReadFile(path)
	if err != nil {
		return verification{"phase35_manifest", false, err.Error()}
	}
	var document struct {
		DatasetFingerprint string `json:"dataset_fingerprint"`
		Artifacts          []struct {
			ArtifactPath   string `json:"artifact_path"`
			ArtifactSHA256 string `json:"artifact_sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return verification{"phase35_manifest", false, err.Error()}
	}
	if len(document.Artifacts) < 4 {
		return verification{"phase35_manifest", false, "expected LightGBM and post-processing artifacts"}
	}
	for _, artifact := range document.Artifacts {
		contents, err := os.ReadFile(artifact.ArtifactPath)
		if err != nil {
			return verification{"phase35_manifest", false, fmt.Sprintf("artifact %s: %v", artifact.ArtifactPath, err)}
		}
		digest := sha256.Sum256(contents)
		if strings.ToLower(artifact.ArtifactSHA256) != hex.EncodeToString(digest[:]) {
			return verification{"phase35_manifest", false, fmt.Sprintf("checksum mismatch for %s", artifact.ArtifactPath)}
		}
	}
	return verification{"phase35_manifest", true, fmt.Sprintf("%d immutable artifacts have matching checksums", len(document.Artifacts))}
}

func checkDatabase(databaseURL, migrationsDir, datasetFingerprint string) verification {
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		return verification{"database_registry", false, err.Error()}
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, migrationsDir); err != nil {
		return verification{"database_registry", false, err.Error()}
	}
	var artifacts, experiments int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM model_artifacts WHERE dataset_fingerprint = $1 AND promotion_status = 'candidate'`, datasetFingerprint).Scan(&artifacts); err != nil {
		return verification{"database_registry", false, err.Error()}
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM model_experiments WHERE dataset_fingerprint = $1 AND status = 'completed'`, datasetFingerprint).Scan(&experiments); err != nil {
		return verification{"database_registry", false, err.Error()}
	}
	var nonCandidate int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM model_artifacts WHERE dataset_fingerprint = $1 AND promotion_status <> 'candidate'`, datasetFingerprint).Scan(&nonCandidate); err != nil {
		return verification{"database_registry", false, err.Error()}
	}
	if artifacts < 4 || experiments < 1 || nonCandidate != 0 {
		return verification{"database_registry", false, fmt.Sprintf("candidate_artifacts=%d completed_experiments=%d non_candidate_artifacts=%d", artifacts, experiments, nonCandidate)}
	}
	return verification{"database_registry", true, fmt.Sprintf("candidate_artifacts=%d completed_experiments=%d", artifacts, experiments)}
}

func checkCredentialHygiene() verification {
	for _, name := range []string{"BET9JA_API_KEY", "BET9JA_TOKEN", "BET9JA_PASSWORD"} {
		if value := os.Getenv(name); value != "" {
			return verification{"credential_hygiene", false, name + " must not be used by verification"}
		}
	}
	return verification{"credential_hygiene", true, "no provider credentials read"}
}
