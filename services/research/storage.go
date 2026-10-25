package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ResearchStorage struct {
	DB  *sql.DB
	Now func() time.Time
}

func NewResearchStorage(db *sql.DB) *ResearchStorage {
	return &ResearchStorage{DB: db, Now: time.Now}
}

func (s *ResearchStorage) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *ResearchStorage) EnsureFeatureDefinition(ctx context.Context, key, version, description, policy string, definition any) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("research storage database is nil")
	}
	definitionJSON, err := jsonValue(definition)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO feature_definitions (feature_key, version, description, point_in_time_policy, definition)
VALUES ($1, $2, $3, $4, $5::jsonb)
ON CONFLICT (feature_key, version) DO UPDATE SET
    description = EXCLUDED.description,
    point_in_time_policy = EXCLUDED.point_in_time_policy,
    definition = EXCLUDED.definition
RETURNING feature_definition_id`, key, version, description, policy, definitionJSON).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure feature definition: %w", err)
	}
	return id, nil
}

func (s *ResearchStorage) InsertFeatureSnapshots(ctx context.Context, featureDefinitionID int64, fingerprint string, vectors []FeatureVector) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("research storage database is nil")
	}
	if len(fingerprint) != 64 {
		return fmt.Errorf("dataset fingerprint must be a 64-character SHA-256 value")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin feature snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, vector := range vectors {
		values, marshalErr := json.Marshal(vector.Values)
		if marshalErr != nil {
			return marshalErr
		}
		provenance, marshalErr := json.Marshal(vector.Provenance)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO feature_snapshots (
    feature_definition_id, dataset_fingerprint, season_id, match_id, round_no,
    cutoff_at, generated_at, values, provenance, point_in_time_verified
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10)
ON CONFLICT (feature_definition_id, dataset_fingerprint, match_id, cutoff_at) DO UPDATE SET
    generated_at = EXCLUDED.generated_at,
    values = EXCLUDED.values,
    provenance = EXCLUDED.provenance,
    point_in_time_verified = EXCLUDED.point_in_time_verified`,
			featureDefinitionID, strings.ToLower(fingerprint), vector.SeasonID.String(), vector.MatchID,
			vector.Round, vector.CutoffAt.UTC(), vector.GeneratedAt.UTC(), string(values), string(provenance), vector.PointInTimeVerified)
		if err != nil {
			return fmt.Errorf("insert feature snapshot %s: %w", vector.MatchID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit feature snapshots: %w", err)
	}
	return nil
}

func (s *ResearchStorage) EnsureModelDefinition(ctx context.Context, modelKey, version, description string, parameters any) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("research storage database is nil")
	}
	parametersJSON, err := jsonValue(parameters)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO model_definitions (model_key, version, description, parameters)
VALUES ($1, $2, $3, $4::jsonb)
ON CONFLICT (model_key, version) DO UPDATE SET
    description = EXCLUDED.description,
    parameters = EXCLUDED.parameters
RETURNING model_definition_id`, modelKey, version, description, parametersJSON).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure model definition: %w", err)
	}
	return id, nil
}

// PersistBacktest stores a complete immutable backtest result. It is optional
// for offline runs, but makes the CLI's output reproducible in the same
// database as the ingestion provenance.
func (s *ResearchStorage) PersistBacktest(ctx context.Context, result BacktestResult, configuration any, oddsPolicy string, codeVersion string) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("research storage database is nil")
	}
	configJSON, err := jsonValue(configuration)
	if err != nil {
		return "", err
	}
	now := s.now()
	var runID string
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO backtest_runs (
    dataset_fingerprint, configuration, cutoff_policy, odds_provenance_policy,
    status, started_at, completed_at, code_version
) VALUES ($1, $2::jsonb, $3, $4, 'completed', $5, $5, $6)
RETURNING backtest_run_id::text`, result.DatasetFingerprint, configJSON,
		result.TrainingPolicy, oddsPolicy, now, codeVersion).Scan(&runID)
	if err != nil {
		return "", fmt.Errorf("insert backtest run: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin backtest persistence: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	modelIDs := make(map[string]int64)
	for _, evaluation := range result.Evaluations {
		var id int64
		err = tx.QueryRowContext(ctx, `
INSERT INTO model_definitions (model_key, version, description, parameters)
VALUES ($1, 'phase2.v1', $2, '{}'::jsonb)
ON CONFLICT (model_key, version) DO UPDATE SET description = EXCLUDED.description
RETURNING model_definition_id`, evaluation.ModelName, "Phase 2 statistical baseline").Scan(&id)
		if err != nil {
			return "", fmt.Errorf("insert model definition %s: %w", evaluation.ModelName, err)
		}
		modelIDs[evaluation.ModelName] = id
	}
	for _, prediction := range result.Predictions {
		modelID, exists := modelIDs[prediction.ModelName]
		if !exists {
			continue
		}
		probabilityJSON, marshalErr := json.Marshal(prediction.Probability)
		if marshalErr != nil {
			return "", marshalErr
		}
		oddsProvenance := map[string]any{"track": prediction.OddsTrack, "point_in_time_verified": prediction.OddsPointInTimeVerified}
		oddsJSON, marshalErr := json.Marshal(oddsProvenance)
		if marshalErr != nil {
			return "", marshalErr
		}
		var oddsObservedAt, sourceTimestampAt any
		if prediction.Odds != nil {
			oddsObservedAt = nullableTime(prediction.Odds.HomeQuote.ObservedAt)
			sourceTimestampAt = nullableTime(prediction.Odds.HomeQuote.SourceTimestampAt)
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO research_predictions (
    backtest_run_id, model_definition_id, season_id, match_id, round_no,
    prediction_as_of, cutoff_at, actual_outcome,
    home_probability, draw_probability, away_probability, probability_payload,
    odds_observed_at, provider_odds_timestamp_at, odds_provenance,
    point_in_time_verified, model_version
) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb,
          $13, $14, $15::jsonb, $16, $17)
ON CONFLICT (backtest_run_id, model_definition_id, match_id, prediction_as_of) DO NOTHING`,
			runID, modelID, prediction.SeasonID.String(), prediction.MatchID, prediction.Round,
			prediction.PredictionAsOf.UTC(), prediction.CutoffAt.UTC(), string(prediction.Actual),
			prediction.Probability.Home, prediction.Probability.Draw, prediction.Probability.Away,
			string(probabilityJSON), oddsObservedAt, sourceTimestampAt, string(oddsJSON),
			prediction.OddsPointInTimeVerified, prediction.ModelVersion)
		if err != nil {
			return "", fmt.Errorf("insert research prediction %s/%s: %w", prediction.ModelName, prediction.MatchID, err)
		}
	}
	for _, evaluation := range result.Evaluations {
		modelID := modelIDs[evaluation.ModelName]
		if err := insertEvaluationMetrics(ctx, tx, runID, modelID, "aggregate", "", 0, "", evaluation.Metrics, evaluation.Confidence); err != nil {
			return "", err
		}
		for seasonID, metrics := range evaluation.BySeason {
			if err := insertEvaluationMetrics(ctx, tx, runID, modelID, "season", seasonID, 0, "", metrics, nil); err != nil {
				return "", err
			}
		}
		for round, metrics := range evaluation.ByRound {
			roundNumber := 0
			_, _ = fmt.Sscanf(round, "%d", &roundNumber)
			if err := insertEvaluationMetrics(ctx, tx, runID, modelID, "round", "", roundNumber, "", metrics, nil); err != nil {
				return "", err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit backtest persistence: %w", err)
	}
	return runID, nil
}

func (s *ResearchStorage) PersistBettingReport(ctx context.Context, report BettingReport, datasetFingerprint, backtestRunID string, configuration any) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("research storage database is nil")
	}
	configJSON, err := jsonValue(configuration)
	if err != nil {
		return "", err
	}
	policy := "verified_point_in_time_only"
	if report.AllowRetrospectiveOdds {
		policy = "retrospective_unverified_availability_explicit_opt_in"
	}
	now := s.now()
	var runID string
	err = s.DB.QueryRowContext(ctx, `
INSERT INTO betting_analysis_runs (
    backtest_run_id, dataset_fingerprint, configuration, odds_provenance_policy,
    status, started_at, completed_at
) VALUES (NULLIF($1, '')::uuid, $2, $3::jsonb, $4, 'completed', $5, $5)
RETURNING betting_analysis_run_id::text`, backtestRunID, datasetFingerprint, configJSON, policy, now).Scan(&runID)
	if err != nil {
		return "", fmt.Errorf("insert betting analysis run: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin betting analysis persistence: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, selection := range report.Selections {
		matchID, parseErr := strconv.ParseInt(selection.MatchID, 10, 64)
		if parseErr != nil || matchID <= 0 {
			return "", fmt.Errorf("invalid betting selection match ID %q", selection.MatchID)
		}
		provenance, marshalErr := json.Marshal(map[string]any{
			"model_name":             selection.ModelName,
			"odds_track":             selection.OddsTrack,
			"probability_difference": selection.ProbabilityDifference,
		})
		if marshalErr != nil {
			return "", marshalErr
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO betting_analysis_selections (
    betting_analysis_run_id, season_id, match_id, round_no, outcome_key,
    model_probability, quoted_decimal_odds, bookmaker_implied_probability,
    expected_value, stake, realized_return, realized_profit,
    odds_observed_at, point_in_time_verified, provenance
) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
ON CONFLICT (betting_analysis_run_id, match_id, outcome_key) DO NOTHING`,
			runID, selection.SeasonID, matchID, selection.Round, string(selection.Outcome),
			selection.ModelProbability, selection.QuotedDecimalOdds, selection.BookmakerImplied,
			selection.ExpectedValue, selection.Stake, selection.RealizedReturn, selection.RealizedProfit,
			nullableTime(selection.OddsObservedAt), selection.PointInTimeVerified,
			string(provenance))
		if err != nil {
			return "", fmt.Errorf("insert betting selection %s/%s: %w", selection.SeasonID, selection.MatchID, err)
		}
	}
	if err := insertBettingMetrics(ctx, tx, runID, "aggregate", "", report.Aggregate); err != nil {
		return "", err
	}
	for _, season := range report.BySeason {
		if err := insertBettingMetrics(ctx, tx, runID, "season", season.SeasonID, season.Metrics); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit betting analysis persistence: %w", err)
	}
	return runID, nil
}

func insertBettingMetrics(ctx context.Context, tx *sql.Tx, runID, scope, seasonID string, metrics BettingMetrics) error {
	values := map[string]float64{
		"selections":       float64(metrics.Selections),
		"hit_rate":         metrics.HitRate,
		"total_stake":      metrics.TotalStake,
		"total_return":     metrics.TotalReturn,
		"profit":           metrics.Profit,
		"roi":              metrics.ROI,
		"maximum_drawdown": metrics.MaximumDrawdown,
	}
	for key, value := range values {
		var seasonValue any
		if seasonID != "" {
			seasonValue = seasonID
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO betting_analysis_metrics (
    betting_analysis_run_id, scope_type, scope_season_id, metric_key,
    metric_value, sample_count
) VALUES ($1::uuid, $2, $3, $4, $5, $6)
ON CONFLICT DO NOTHING`, runID, scope, seasonValue, key, value, metrics.Selections)
		if err != nil {
			return fmt.Errorf("insert betting metric %s/%s: %w", scope, key, err)
		}
	}
	return nil
}

func insertEvaluationMetrics(ctx context.Context, tx *sql.Tx, runID string, modelID int64, scope, seasonID string, round int, market string, metrics Metrics, intervals map[string]ConfidenceInterval) error {
	values := map[string]float64{
		"log_loss":                   metrics.LogLoss,
		"multiclass_brier":           metrics.Brier,
		"accuracy":                   metrics.Accuracy,
		"expected_calibration_error": metrics.ECE,
	}
	for key, value := range values {
		var low, high any
		if interval, ok := intervals[key]; ok {
			low, high = interval.Low, interval.High
		}
		var roundValue any
		if round > 0 {
			roundValue = round
		}
		var seasonValue any
		if seasonID != "" {
			seasonValue = seasonID
		}
		var marketValue any
		if market != "" {
			marketValue = market
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO evaluation_metrics (
    backtest_run_id, model_definition_id, scope_type, scope_season_id,
    scope_round_no, market_key, metric_key, metric_value, sample_count,
    confidence_low, confidence_high
) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT DO NOTHING`, runID, modelID, scope, seasonValue, roundValue, marketValue, key, value, metrics.Count, low, high)
		if err != nil {
			return fmt.Errorf("insert evaluation metric %s/%s: %w", scope, key, err)
		}
	}
	return nil
}

func jsonValue(value any) (string, error) {
	if value == nil {
		return `{}`, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JSON value: %w", err)
	}
	return string(encoded), nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
