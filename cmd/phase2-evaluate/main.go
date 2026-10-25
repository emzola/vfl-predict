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

type phase2Report struct {
	GeneratedAt            time.Time               `json:"generated_at"`
	DatasetFingerprint     string                  `json:"dataset_fingerprint"`
	RawSeasonCount         int                     `json:"raw_season_count"`
	RawMatchCount          int                     `json:"raw_match_count"`
	ExcludedIncompleteIDs  []string                `json:"excluded_incomplete_season_ids"`
	SeasonCount            int                     `json:"season_count"`
	MatchCount             int                     `json:"match_count"`
	FinalMatchCount        int                     `json:"final_match_count"`
	FeatureVectorCount     int                     `json:"feature_vector_count"`
	HistoricalOddsPolicy   string                  `json:"historical_odds_policy"`
	Backtest               research.BacktestResult `json:"backtest"`
	Betting                research.BettingReport  `json:"betting"`
	PersistedBacktestRunID string                  `json:"persisted_backtest_run_id,omitempty"`
	PersistedBettingRunID  string                  `json:"persisted_betting_analysis_run_id,omitempty"`
	Warnings               []string                `json:"warnings"`
}

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "SQL migrations directory")
	reportPath := flag.String("report", "tmp/phase2-report.json", "JSON report path")
	modelsFlag := flag.String("models", strings.Join(research.BaselineModelNames(), ","), "comma-separated baseline model names")
	minTraining := flag.Int("min-training-seasons", 1, "minimum completed seasons before evaluation")
	allowRetrospective := flag.Bool("allow-retrospective-odds", false, "include historical odds returned after settlement as a labeled retrospective track")
	bettingModel := flag.String("betting-model", "statistical_bookmaker_combination", "model used for research-only betting analysis")
	persist := flag.Bool("persist", false, "persist feature, backtest, and metric artifacts in PostgreSQL")
	flag.Parse()

	if strings.TrimSpace(*databaseURL) == "" {
		fatal("database URL is required; set DATABASE_URL or -database-url")
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
	rawDataset, err := research.LoadDataset(ctx, db)
	if err != nil {
		fatal(err.Error())
	}
	dataset, excluded := rawDataset.CompleteSeasons()
	featureConfig := research.DefaultFeatureConfig()
	features, err := research.BuildFeatureVectors(dataset, featureConfig)
	if err != nil {
		fatal(fmt.Sprintf("build features: %v", err))
	}
	models := split(*modelsFlag)
	backtestConfig := research.DefaultBacktestConfig()
	backtestConfig.Models = models
	backtestConfig.MinTrainingSeasons = *minTraining
	backtestConfig.AllowRetrospectiveOdds = *allowRetrospective
	backtest, err := research.RunBacktest(dataset, backtestConfig)
	if err != nil {
		fatal(fmt.Sprintf("run backtest: %v", err))
	}
	bettingPredictions := make([]research.BacktestPrediction, 0)
	for _, prediction := range backtest.Predictions {
		if prediction.ModelName == *bettingModel {
			bettingPredictions = append(bettingPredictions, prediction)
		}
	}
	bettingConfig := research.DefaultBettingConfig()
	bettingConfig.AllowRetrospectiveOdds = *allowRetrospective
	betting := research.AnalyzeBetting(bettingPredictions, bettingConfig)

	warnings := make([]string, 0)
	if *allowRetrospective {
		warnings = append(warnings, "retrospective odds are not evidence of availability at historical prediction cutoffs")
	}
	if len(backtest.SeasonsEvaluated) == 0 {
		warnings = append(warnings, "no held-out season was available; add another completed season before interpreting model metrics")
	}
	if len(excluded) > 0 {
		warnings = append(warnings, fmt.Sprintf("excluded %d incomplete seasons from research evaluation; inspect ingestion coverage before using them", len(excluded)))
	}
	finalMatches := 0
	for _, match := range dataset.Matches {
		if match.Final() {
			finalMatches++
		}
	}
	output := phase2Report{
		GeneratedAt:           time.Now().UTC(),
		DatasetFingerprint:    dataset.Fingerprint(),
		RawSeasonCount:        len(rawDataset.Seasons),
		RawMatchCount:         len(rawDataset.Matches),
		ExcludedIncompleteIDs: stringifySeasonIDs(excluded),
		SeasonCount:           len(dataset.Seasons),
		MatchCount:            len(dataset.Matches),
		FinalMatchCount:       finalMatches,
		FeatureVectorCount:    len(features),
		HistoricalOddsPolicy:  "only verified odds observations are point-in-time eligible; retrospective odds require explicit opt-in",
		Backtest:              backtest,
		Betting:               betting,
		Warnings:              warnings,
	}
	if *persist {
		storage := research.NewResearchStorage(db)
		definitionID, persistErr := storage.EnsureFeatureDefinition(ctx, featureConfig.FeatureKey, featureConfig.Version,
			"Deterministic pre-cutoff team, form, head-to-head, standings, rest, and verified odds features",
			"final_results_and_verified_observations_before_cutoff", map[string]any{
				"round_safety_buffer_seconds": int(featureConfig.RoundSafetyBuffer / time.Second),
				"feature_version":             featureConfig.Version,
			})
		if persistErr != nil {
			fatal(persistErr.Error())
		}
		if persistErr = storage.InsertFeatureSnapshots(ctx, definitionID, output.DatasetFingerprint, features); persistErr != nil {
			fatal(persistErr.Error())
		}
		runID, persistErr := storage.PersistBacktest(ctx, backtest, map[string]any{
			"models":                   models,
			"min_training_seasons":     *minTraining,
			"allow_retrospective_odds": *allowRetrospective,
		}, "retrospective_allowed_only_by_explicit_flag", "phase2.v1")
		if persistErr != nil {
			fatal(persistErr.Error())
		}
		output.PersistedBacktestRunID = runID
		bettingRunID, persistErr := storage.PersistBettingReport(ctx, betting, output.DatasetFingerprint, runID, map[string]any{
			"model":                    *bettingModel,
			"allow_retrospective_odds": *allowRetrospective,
			"minimum_expected_value":   bettingConfig.MinimumExpectedValue,
			"stake":                    bettingConfig.Stake,
		})
		if persistErr != nil {
			fatal(persistErr.Error())
		}
		output.PersistedBettingRunID = bettingRunID
	}
	if err := writeJSON(*reportPath, output); err != nil {
		fatal(err.Error())
	}
	encoded, _ := json.MarshalIndent(map[string]any{
		"report":             *reportPath,
		"seasons":            len(dataset.Seasons),
		"matches":            len(dataset.Matches),
		"final_matches":      finalMatches,
		"features":           len(features),
		"evaluated_seasons":  len(backtest.SeasonsEvaluated),
		"betting_selections": betting.Aggregate.Selections,
	}, "", "  ")
	fmt.Println(string(encoded))
}

func split(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func stringifySeasonIDs(ids []provider.SeasonID) []string {
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
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
