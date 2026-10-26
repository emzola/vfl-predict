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
	"bet9ja-vfl/services/provider"
	"bet9ja-vfl/services/research"
)

type phase6Report struct {
	SchemaVersion        string                       `json:"schema_version"`
	GeneratedAt          time.Time                    `json:"generated_at"`
	DatasetFingerprint   string                       `json:"dataset_fingerprint"`
	RawSeasonCount       int                          `json:"raw_season_count"`
	EvaluatedSeasonCount int                          `json:"evaluated_season_count"`
	FinalMatchCount      int                          `json:"final_match_count"`
	ExcludedSeasons      []string                     `json:"excluded_incomplete_seasons"`
	DynamicReplay        research.DynamicReplayResult `json:"dynamic_replay"`
	BaselineComparison   research.BacktestResult      `json:"baseline_comparison"`
	Warnings             []string                     `json:"warnings"`
}

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "SQL migrations directory")
	reportPath := flag.String("report", "tmp/phase6-replay-report.json", "JSON report path")
	minTraining := flag.Int("min-training-seasons", 1, "completed seasons before evaluation")
	bootstrap := flag.Int("bootstrap-replicates", 400, "season-block bootstrap replicates")
	flag.Parse()
	if strings.TrimSpace(*databaseURL) == "" {
		fatal("set DATABASE_URL or -database-url")
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
	raw, err := research.LoadDataset(ctx, db)
	if err != nil {
		fatal(err.Error())
	}
	dataset, excluded := raw.CompleteSeasons()
	dynamicConfig := research.DefaultDynamicReplayConfig()
	dynamicConfig.MinTrainingSeasons = *minTraining
	dynamicConfig.BootstrapReplicates = *bootstrap
	dynamic, err := research.RunDynamicBayesianReplay(dataset, dynamicConfig)
	if err != nil {
		fatal(fmt.Sprintf("dynamic replay: %v", err))
	}
	baselineConfig := research.DefaultBacktestConfig()
	baselineConfig.MinTrainingSeasons = *minTraining
	baselineConfig.BootstrapReplicates = *bootstrap
	baselineConfig.AllowRetrospectiveOdds = false
	baselineConfig.Models = []string{"bookmaker_first", "poisson", "bookmaker_implied", research.DynamicBayesianModelName}
	comparison, err := research.RunBacktest(dataset, baselineConfig)
	if err != nil {
		fatal(fmt.Sprintf("baseline comparison: %v", err))
	}
	warnings := []string{
		"This is retrospective walk-forward research; it is not a production promotion decision.",
		"Historical odds are excluded unless independently verified before the simulated cutoff.",
		"The dynamic model uses a deterministic approximate-Bayesian filter rather than per-round MCMC.",
	}
	if len(dynamic.SeasonsEvaluated) == 0 {
		warnings = append(warnings, "no held-out seasons were available")
	}
	if len(excluded) > 0 {
		warnings = append(warnings, fmt.Sprintf("excluded %d incomplete seasons", len(excluded)))
	}
	finalMatches := 0
	for _, match := range dataset.Matches {
		if match.Final() {
			finalMatches++
		}
	}
	report := phase6Report{
		SchemaVersion:        "phase6.replay.v1",
		GeneratedAt:          time.Now().UTC(),
		DatasetFingerprint:   dataset.Fingerprint(),
		RawSeasonCount:       len(raw.Seasons),
		EvaluatedSeasonCount: len(dynamic.SeasonsEvaluated),
		FinalMatchCount:      finalMatches,
		ExcludedSeasons:      seasonStrings(excluded),
		DynamicReplay:        dynamic,
		BaselineComparison:   comparison,
		Warnings:             warnings,
	}
	if err := writeJSON(*reportPath, report); err != nil {
		fatal(err.Error())
	}
	for _, evaluation := range comparison.Evaluations {
		fmt.Printf("%-32s log_loss=%.6f brier=%.6f accuracy=%.4f n=%d\n", evaluation.ModelName, evaluation.Metrics.LogLoss, evaluation.Metrics.Brier, evaluation.Metrics.Accuracy, evaluation.Metrics.Count)
	}
	fmt.Printf("dynamic markets: 1x2_log_loss=%.6f btts_log_loss=%.6f over_2_5_log_loss=%.6f correct_score_log_loss=%.6f\n", dynamic.Evaluation.Metrics.OneXTwo.LogLoss, dynamic.Evaluation.Metrics.BTTS.LogLoss, dynamic.Evaluation.Metrics.OverUnder25.LogLoss, dynamic.Evaluation.Metrics.CorrectScoreLogLoss)
	fmt.Printf("report=%s seasons=%d final_matches=%d\n", *reportPath, len(dataset.Seasons), finalMatches)
}

func seasonStrings(ids []provider.SeasonID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, id.String())
	}
	return result
}

func writeJSON(path string, value any) error {
	if directory := filepath.Dir(path); directory != "." && directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o600)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "phase6-evaluate:", message)
	os.Exit(1)
}
