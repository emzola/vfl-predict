package prediction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
	"bet9ja-vfl/services/research"
)

type Status string

const (
	StatusPending      Status = "pending"
	StatusProvisional  Status = "provisional"
	StatusFrozen       Status = "frozen"
	StatusSettled      Status = "settled"
	StatusStale        Status = "stale"
	StatusUnavailable  Status = "unavailable"
	StatusMissedCutoff Status = "missed_cutoff"
)

type Config struct {
	ModelName           string
	PolicyVersion       string
	SafetyBuffer        time.Duration
	MinimumTrainingData int
	RequireVerifiedOdds bool
	FeatureConfig       research.FeatureConfig
}

func DefaultConfig() Config {
	return Config{
		ModelName:           "bookmaker_first",
		PolicyVersion:       "phase5.pre_match.bookmaker_first.v1",
		SafetyBuffer:        30 * time.Second,
		MinimumTrainingData: 1,
		RequireVerifiedOdds: true,
		FeatureConfig:       research.DefaultFeatureConfig(),
	}
}

func (c Config) normalize() Config {
	defaults := DefaultConfig()
	if strings.TrimSpace(c.ModelName) == "" {
		c.ModelName = defaults.ModelName
	}
	if strings.TrimSpace(c.PolicyVersion) == "" {
		c.PolicyVersion = defaults.PolicyVersion
	}
	if c.SafetyBuffer <= 0 {
		c.SafetyBuffer = defaults.SafetyBuffer
	}
	if c.MinimumTrainingData < 0 {
		c.MinimumTrainingData = defaults.MinimumTrainingData
	}
	if c.FeatureConfig.FeatureKey == "" {
		c.FeatureConfig = research.DefaultFeatureConfig()
	}
	return c
}

type RoundReport struct {
	SeasonID        string    `json:"season_id"`
	Round           int       `json:"round"`
	RunID           string    `json:"run_id,omitempty"`
	Status          Status    `json:"status"`
	ModelName       string    `json:"model_name"`
	ModelVersion    string    `json:"model_version"`
	InputVersion    string    `json:"input_version"`
	PredictionAsOf  time.Time `json:"prediction_as_of"`
	CutoffAt        time.Time `json:"cutoff_at"`
	EarliestKickoff time.Time `json:"earliest_kickoff"`
	FixtureCount    int       `json:"fixture_count"`
	GeneratedCount  int       `json:"generated_count"`
	PointInTimeOdds int       `json:"point_in_time_odds_count"`
	BookmakerUsed   int       `json:"bookmaker_used_count"`
	PoissonFallback int       `json:"poisson_fallback_count"`
	Warnings        []string  `json:"warnings,omitempty"`
	ExistingRun     bool      `json:"existing_run"`
}

type Engine struct {
	DB  *sql.DB
	Now func() time.Time
}

func NewEngine(db *sql.DB) *Engine { return &Engine{DB: db, Now: time.Now} }

func (e *Engine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

// GenerateRound creates one complete provisional or frozen round. The entire
// eight-fixture insert occurs in one transaction; malformed, incomplete, or
// late rounds are represented by a status run and never published partially.
func (e *Engine) GenerateRound(ctx context.Context, seasonID string, round int, requested Config, freeze bool) (RoundReport, error) {
	cfg := requested.normalize()
	if e == nil || e.DB == nil {
		return RoundReport{}, errors.New("prediction engine database is nil")
	}
	if strings.TrimSpace(seasonID) == "" || round < 1 || round > 30 {
		return RoundReport{}, errors.New("season and round are required")
	}
	now := e.now()
	dynamicModel := isDynamicModelName(cfg.ModelName)
	var dataset research.Dataset
	var err error
	if dynamicModel {
		dataset, err = research.LoadFixtureDataset(ctx, e.DB)
	} else {
		dataset, err = research.LoadDataset(ctx, e.DB)
	}
	if err != nil {
		return RoundReport{}, fmt.Errorf("load prediction dataset: %w", err)
	}
	if dynamicModel {
		// The dynamic model fits from completed fixtures/results. Load odds only
		// for the eight target fixtures so point-in-time market metadata remains
		// available without scanning the full historical odds archive.
		targetIDs := make([]provider.MatchID, 0, 8)
		for _, match := range dataset.Matches {
			if match.SeasonID.String() == seasonID && match.Round == round {
				targetIDs = append(targetIDs, match.MatchID)
			}
		}
		oddsByMatch, oddsErr := research.LoadOddsForMatches(ctx, e.DB, targetIDs)
		if oddsErr != nil {
			return RoundReport{}, fmt.Errorf("load target odds: %w", oddsErr)
		}
		for index := range dataset.Matches {
			if odds, ok := oddsByMatch[dataset.Matches[index].MatchID]; ok {
				dataset.Matches[index].Odds = odds
			}
		}
	}
	targets := make([]research.MatchRecord, 0, 8)
	for _, match := range dataset.Matches {
		if match.SeasonID.String() == seasonID && match.Round == round {
			targets = append(targets, match)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].MatchID < targets[j].MatchID })
	report := RoundReport{SeasonID: seasonID, Round: round, ModelName: cfg.ModelName, PredictionAsOf: now, FixtureCount: len(targets)}
	if len(targets) != 8 {
		report.Status = StatusUnavailable
		report.Warnings = append(report.Warnings, fmt.Sprintf("round has %d fixtures; eight are required", len(targets)))
		return e.persistUnavailable(ctx, report, cfg, "round_fixture_count_mismatch")
	}
	earliest := targets[0].ScheduledAt
	for _, target := range targets[1:] {
		if target.ScheduledAt.Before(earliest) {
			earliest = target.ScheduledAt
		}
	}
	cutoff := earliest.Add(-cfg.SafetyBuffer)
	report.EarliestKickoff = earliest
	report.CutoffAt = cutoff
	if !now.Before(cutoff) {
		report.Status = StatusMissedCutoff
		report.Warnings = append(report.Warnings, "prediction generation time is not before the configured cutoff")
		return e.persistUnavailable(ctx, report, cfg, "prediction_cutoff_missed")
	}
	training := make([]research.MatchRecord, 0, len(dataset.Matches))
	asOfMatches := make([]research.MatchRecord, 0, len(dataset.Matches))
	targetIDs := make(map[provider.MatchID]struct{}, len(targets))
	for _, target := range targets {
		targetIDs[target.MatchID] = struct{}{}
	}
	for _, match := range dataset.Matches {
		if _, target := targetIDs[match.MatchID]; target {
			clone := match
			clone.HomeScore = nil
			clone.AwayScore = nil
			clone.Odds = eligibleOdds(match.Odds, cutoff, cfg.RequireVerifiedOdds)
			asOfMatches = append(asOfMatches, clone)
			continue
		}
		if match.Final() && match.ScheduledAt.Before(cutoff) {
			training = append(training, match)
			clone := match
			clone.Odds = nil
			asOfMatches = append(asOfMatches, clone)
		}
	}
	// The dynamic Bayesian candidate has a regularized prior and can produce a
	// valid early-season distribution even when there are no completed matches
	// yet. Other policies retain the existing minimum-training-data guard.
	if len(training) < cfg.MinimumTrainingData && cfg.ModelName != research.DynamicBayesianModelName {
		report.Status = StatusUnavailable
		report.Warnings = append(report.Warnings, fmt.Sprintf("only %d eligible historical matches; need %d", len(training), cfg.MinimumTrainingData))
		return e.persistUnavailable(ctx, report, cfg, "insufficient_training_data")
	}
	inputDataset := research.NewDataset(asOfMatches, dataset.Seasons)
	inputVersion := inputDataset.Fingerprint()
	report.InputVersion = inputVersion
	model, err := research.NewBaselineModel(cfg.ModelName, false)
	if err != nil {
		return RoundReport{}, err
	}
	if err := model.Fit(training); err != nil {
		return RoundReport{}, fmt.Errorf("fit %s: %w", cfg.ModelName, err)
	}
	if dynamic, ok := model.(*research.DynamicBayesianModel); ok {
		dynamic.PrepareSeason(provider.SeasonID(seasonID))
	}
	modelVersion := model.Version()
	report.ModelVersion = modelVersion
	if freeze {
		report.Status = StatusFrozen
	} else {
		report.Status = StatusProvisional
	}
	runType := string(report.Status)
	idempotencyKey := fmt.Sprintf("prediction:%s:%d:%s:%s:%s:%s", seasonID, round, modelVersion, inputVersion, cfg.PolicyVersion, runType)
	modelParameters := map[string]any{"policy": cfg.PolicyVersion}
	if dynamic, ok := model.(*research.DynamicBayesianModel); ok {
		modelParameters["phase6"] = dynamic.Parameters()
	}
	modelID, err := research.NewResearchStorage(e.DB).EnsureModelDefinition(ctx, cfg.ModelName, modelVersion, predictionModelDescription(cfg.ModelName), modelParameters)
	if err != nil {
		return RoundReport{}, err
	}
	if dynamic, ok := model.(*research.DynamicBayesianModel); ok {
		if _, err := research.NewResearchStorage(e.DB).SaveDynamicCheckpoint(ctx, modelID, seasonID, round, cutoff, inputVersion, dynamic, map[string]any{
			"prediction_policy": cfg.PolicyVersion,
			"model_version":     modelVersion,
			"prediction_as_of":  now,
		}); err != nil {
			return RoundReport{}, err
		}
	}
	for index := range targets {
		clone := targets[index]
		clone.HomeScore = nil
		clone.AwayScore = nil
		clone.Odds = eligibleOdds(clone.Odds, cutoff, cfg.RequireVerifiedOdds)
		if _, err := research.BuildFeatureVector(inputDataset, clone, cutoff, cfg.FeatureConfig); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("match %s features: %v", clone.MatchID, err))
		}
	}
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return RoundReport{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var runID, existingStatus string
	var existingGenerated int
	var existed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM prediction_runs WHERE idempotency_key = $1)`, idempotencyKey).Scan(&existed); err != nil {
		return RoundReport{}, err
	}
	err = tx.QueryRowContext(ctx, `
INSERT INTO prediction_runs (
    season_id, round_no, model_definition_id, model_version, run_type, status,
    prediction_as_of, input_cutoff_at, earliest_kickoff_at, odds_observation_cutoff_at,
    input_dataset_version, prediction_policy, idempotency_key, generated_fixture_count, manifest
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $8, $10, $11, $12, $13, $14::jsonb)
ON CONFLICT (idempotency_key) DO UPDATE SET updated_at = now()
RETURNING prediction_run_id::text, status, generated_fixture_count`, seasonID, round, modelID, modelVersion, runType,
		string(report.Status), now, cutoff, earliest, inputVersion, cfg.PolicyVersion, idempotencyKey,
		map[bool]int{true: 8, false: 0}[freeze], fmt.Sprintf(`{"freeze_requested":%t,"require_verified_odds":%t}`, freeze, cfg.RequireVerifiedOdds)).Scan(&runID, &existingStatus, &existingGenerated)
	if err != nil {
		return RoundReport{}, fmt.Errorf("create prediction run: %w", err)
	}
	if existed && (existingStatus == string(StatusFrozen) || (existingStatus == string(StatusProvisional) && existingGenerated >= 8)) {
		report.RunID = runID
		report.Status = Status(existingStatus)
		report.GeneratedCount = existingGenerated
		report.ExistingRun = true
		if err := tx.Commit(); err != nil {
			return RoundReport{}, err
		}
		return report, nil
	}
	pointOdds := 0
	for _, target := range targets {
		clone := target
		clone.HomeScore = nil
		clone.AwayScore = nil
		clone.Odds = eligibleOdds(target.Odds, cutoff, cfg.RequireVerifiedOdds)
		line, hasOdds := research.FindOneXTwoOddsAt(clone, cutoff)
		probability := model.Predict(clone)
		if err := probability.Validate(1e-8); err != nil {
			return RoundReport{}, fmt.Errorf("model %s produced invalid probability for match %s: %w", cfg.ModelName, target.MatchID, err)
		}
		selectedModel := model.Name()
		selectedVersion := model.Version()
		fallbackReason := ""
		if policy, ok := model.(*research.BookmakerFirstModel); ok {
			if hasOdds {
				selectedModel = "bookmaker_implied"
				selectedVersion = "phase2.v1"
				report.BookmakerUsed++
			} else {
				selectedModel = "poisson"
				if policy.Statistical != nil {
					selectedVersion = policy.Statistical.Version()
				}
				fallbackReason = "no_verified_pre_cutoff_1x2_line"
				report.PoissonFallback++
			}
		}
		payload := map[string]any{
			"probability":        probability,
			"model":              selectedModel,
			"model_version":      selectedVersion,
			"prediction_policy":  cfg.ModelName,
			"input_version":      inputVersion,
			"point_in_time_odds": hasOdds,
		}
		if fallbackReason != "" {
			payload["fallback_reason"] = fallbackReason
		}
		switch selected := model.(type) {
		case *research.PoissonGoalModel:
			payload["secondary"] = selected.PredictGoals(clone, 10)
			payload["secondary_model"] = "poisson"
		case *research.BookmakerFirstModel:
			payload["secondary"] = selected.PoissonGoals(clone, 10)
			payload["secondary_model"] = "poisson"
		case *research.DynamicBayesianModel:
			payload["secondary"] = selected.PredictGoals(clone, selected.Config.MaxScore)
			payload["secondary_model"] = research.DynamicBayesianModelName
			payload["dynamic_team_strength"] = selected.Diagnostics(clone)
		}
		feature, featureErr := research.BuildFeatureVector(inputDataset, clone, cutoff, cfg.FeatureConfig)
		if featureErr == nil {
			payload["feature_values"] = feature.Values
			payload["feature_version"] = feature.FeatureVersion
		}
		if hasOdds {
			pointOdds++
		} else if cfg.ModelName == "bookmaker_implied" || cfg.ModelName == "bookmaker" {
			report.Warnings = append(report.Warnings, fmt.Sprintf("match %s has no verified 1X2 odds before cutoff", target.MatchID))
		}
		oddsIDs, oddsObservedAt, providerOddsAt, oddsErr := e.eligibleOddsMetadata(ctx, target.MatchID, cutoff)
		if oddsErr != nil {
			return RoundReport{}, oddsErr
		}
		if hasOdds {
			payload["bookmaker"] = map[string]any{
				"home": line.Home, "draw": line.Draw, "away": line.Away,
				"implied": line.ImpliedProbabilities(), "overround": line.Overround,
			}
		}
		payloadJSON, _ := json.Marshal(payload)
		oddsJSON, _ := json.Marshal(oddsIDs)
		status := string(report.Status)
		var frozenAt any
		if freeze {
			frozenAt = now
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO prediction_revisions (
    prediction_run_id, season_id, match_id, round_no, revision_no, status,
    prediction_as_of, cutoff_at, home_probability, draw_probability, away_probability,
    probability_payload, odds_snapshot_ids, odds_observed_at, provider_odds_timestamp_at,
    point_in_time_verified, eligible, frozen_at
) VALUES ($1::uuid, $2, $3, $4, 1, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb,
          NULLIF($13, '')::timestamptz, NULLIF($14, '')::timestamptz, $15, true, $16)`,
			runID, seasonID, target.MatchID, round, status, now, cutoff,
			probability.Home, probability.Draw, probability.Away, string(payloadJSON), string(oddsJSON),
			nullableTime(oddsObservedAt), nullableTime(providerOddsAt), hasOdds && line.PointInTimeVerified, frozenAt)
		if err != nil {
			return RoundReport{}, fmt.Errorf("insert prediction revision %s: %w", target.MatchID, err)
		}
	}
	_, err = tx.ExecContext(ctx, `
UPDATE prediction_runs
SET generated_fixture_count = 8, frozen_at = CASE WHEN $2 THEN $3 ELSE frozen_at END, updated_at = now()
WHERE prediction_run_id = $1::uuid`, runID, freeze, now)
	if err != nil {
		return RoundReport{}, err
	}
	if err := tx.Commit(); err != nil {
		return RoundReport{}, fmt.Errorf("commit prediction round: %w", err)
	}
	if err := e.upsertRoundState(ctx, seasonID, round, earliest, cutoff, report.Status); err != nil {
		return RoundReport{}, err
	}
	report.RunID = runID
	report.GeneratedCount = 8
	report.PointInTimeOdds = pointOdds
	return report, nil
}

func eligibleOdds(quotes []research.OddsQuote, cutoff time.Time, requireVerified bool) []research.OddsQuote {
	result := make([]research.OddsQuote, 0, len(quotes))
	for _, quote := range quotes {
		if requireVerified && !quote.PointInTimeVerified {
			continue
		}
		if quote.ObservedAt.IsZero() || quote.ObservedAt.After(cutoff) {
			continue
		}
		result = append(result, quote)
	}
	return result
}

func predictionModelDescription(modelName string) string {
	if isDynamicModelName(modelName) {
		return "Phase 6 unified dynamic opponent-adjusted rating and regularized Bayesian attack/defense scoreline model"
	}
	if strings.EqualFold(strings.TrimSpace(modelName), "bookmaker_first") || strings.EqualFold(strings.TrimSpace(modelName), "market_first") || strings.EqualFold(strings.TrimSpace(modelName), "production") {
		return "Phase 5 bookmaker-first production policy with verified pre-cutoff Poisson fallback"
	}
	return "Phase 3 statistical baseline prediction"
}

func isDynamicModelName(modelName string) bool {
	trimmed := strings.TrimSpace(modelName)
	return strings.EqualFold(trimmed, research.DynamicBayesianModelName) ||
		strings.EqualFold(trimmed, "dynamic_bayesian") ||
		strings.EqualFold(trimmed, "phase6")
}

func nullableTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func (e *Engine) eligibleOddsMetadata(ctx context.Context, matchID provider.MatchID, cutoff time.Time) ([]string, time.Time, time.Time, error) {
	rows, err := e.DB.QueryContext(ctx, `
SELECT DISTINCT ON (o.odds_snapshot_id)
       o.odds_snapshot_id::text,
       COALESCE(oo.observed_at, o.last_observed_at),
       COALESCE(oo.source_timestamp_at, o.source_timestamp_at)
FROM odds_snapshots o
LEFT JOIN odds_snapshot_observations oo ON oo.odds_snapshot_id = o.odds_snapshot_id
WHERE o.match_id = $1 AND o.odds_valid = true
  AND COALESCE(oo.point_in_time_verified, o.point_in_time_verified, false) = true
  AND COALESCE(oo.observed_at, o.last_observed_at) <= $2
ORDER BY o.odds_snapshot_id, COALESCE(oo.observed_at, o.last_observed_at) DESC,
         COALESCE(oo.source_timestamp_at, o.source_timestamp_at) DESC`, matchID, cutoff.UTC())
	if err != nil {
		return nil, time.Time{}, time.Time{}, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	var observed, providerTime time.Time
	for rows.Next() {
		var id string
		var observedAt, sourceAt time.Time
		if err := rows.Scan(&id, &observedAt, &sourceAt); err != nil {
			return nil, time.Time{}, time.Time{}, err
		}
		ids = append(ids, id)
		if observed.IsZero() {
			observed = observedAt.UTC()
			providerTime = sourceAt.UTC()
		}
	}
	return ids, observed, providerTime, rows.Err()
}

func (e *Engine) persistUnavailable(ctx context.Context, report RoundReport, cfg Config, reason string) (RoundReport, error) {
	modelVersion := "unavailable"
	if model, err := research.NewBaselineModel(cfg.ModelName, false); err == nil {
		modelVersion = model.Version()
	}
	report.ModelVersion = modelVersion
	report.InputVersion = "unavailable"
	modelID, err := research.NewResearchStorage(e.DB).EnsureModelDefinition(ctx, cfg.ModelName, modelVersion, predictionModelDescription(cfg.ModelName), map[string]any{"policy": cfg.PolicyVersion})
	if err != nil {
		return report, err
	}
	earliest := report.EarliestKickoff
	if earliest.IsZero() {
		earliest = report.PredictionAsOf
	}
	cutoff := report.CutoffAt
	if cutoff.IsZero() {
		cutoff = earliest.Add(-cfg.SafetyBuffer)
	}
	idempotencyKey := fmt.Sprintf("prediction:%s:%d:%s:%s:%s", report.SeasonID, report.Round, modelVersion, cfg.PolicyVersion, reason)
	var runID string
	err = e.DB.QueryRowContext(ctx, `
INSERT INTO prediction_runs (
    season_id, round_no, model_definition_id, model_version, run_type, status,
    prediction_as_of, input_cutoff_at, earliest_kickoff_at, input_dataset_version,
    prediction_policy, idempotency_key, failure_reason, manifest
) VALUES ($1, $2, $3, $4, 'manual', $5, $6, $7, $8, 'unavailable', $9, $10, $11, $12::jsonb)
ON CONFLICT (idempotency_key) DO UPDATE SET updated_at = now()
RETURNING prediction_run_id::text`, report.SeasonID, report.Round, modelID, modelVersion,
		string(report.Status), report.PredictionAsOf, cutoff, earliest, cfg.PolicyVersion,
		idempotencyKey, reason, fmt.Sprintf(`{"reason":%q}`, reason)).Scan(&runID)
	if err != nil {
		return report, err
	}
	report.RunID = runID
	// A missed or incomplete round is visible and auditable, but it is never
	// represented as a complete prediction because no revision rows are made.
	_ = e.upsertRoundState(ctx, report.SeasonID, report.Round, earliest, cutoff, report.Status)
	return report, nil
}

func (e *Engine) upsertRoundState(ctx context.Context, seasonID string, round int, kickoff, cutoff time.Time, status Status) error {
	_, err := e.DB.ExecContext(ctx, `
INSERT INTO round_states (season_id, round_no, earliest_kickoff_at, betting_cutoff_at, state, freshness)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)
ON CONFLICT (season_id, round_no) DO UPDATE SET
    earliest_kickoff_at = EXCLUDED.earliest_kickoff_at,
    betting_cutoff_at = EXCLUDED.betting_cutoff_at,
    state = CASE
        WHEN round_states.state = 'settled' THEN 'settled'
        WHEN round_states.state = 'frozen' AND EXCLUDED.state <> 'settled' THEN 'frozen'
        ELSE EXCLUDED.state
    END,
    updated_at = now()`, seasonID, round, kickoff.UTC(), cutoff.UTC(), string(status), `{"prediction_engine":"phase3"}`)
	return err
}

// MarkStale preserves the latest provisional rows while making their
// operational status explicit after a refresh failure or freshness timeout.
// Frozen and settled runs are never downgraded.
func (e *Engine) MarkStale(ctx context.Context, runID, reason string) error {
	if e == nil || e.DB == nil {
		return errors.New("prediction engine database is nil")
	}
	_, err := e.DB.ExecContext(ctx, `
UPDATE prediction_runs
SET status = 'stale', failure_reason = NULLIF($2, ''), updated_at = now()
WHERE prediction_run_id = $1::uuid AND status = 'provisional'`, runID, reason)
	if err != nil {
		return err
	}
	_, err = e.DB.ExecContext(ctx, `
UPDATE prediction_revisions
SET status = 'stale', ineligible_reason = NULLIF($2, '')
WHERE prediction_run_id = $1::uuid AND status = 'provisional'`, runID, reason)
	return err
}
