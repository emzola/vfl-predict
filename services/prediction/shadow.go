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

// ShadowRoundReport is deliberately separate from RoundReport. Shadow rows
// never enter prediction_runs and therefore cannot be returned as official,
// provisional, or frozen production predictions.
type ShadowRoundReport struct {
	ShadowRunID     string    `json:"shadow_run_id,omitempty"`
	SeasonID        string    `json:"season_id"`
	Round           int       `json:"round"`
	Status          string    `json:"status"`
	ModelName       string    `json:"model_name"`
	ModelVersion    string    `json:"model_version"`
	InputVersion    string    `json:"input_version"`
	PredictionAsOf  time.Time `json:"prediction_as_of"`
	CutoffAt        time.Time `json:"cutoff_at"`
	EarliestKickoff time.Time `json:"earliest_kickoff"`
	FixtureCount    int       `json:"fixture_count"`
	GeneratedCount  int       `json:"generated_count"`
	ExistingRun     bool      `json:"existing_run"`
	Warnings        []string  `json:"warnings,omitempty"`
}

// GenerateShadowRound runs the Phase 6 candidate against a real round's
// point-in-time inputs without touching production prediction tables. It is
// intended for controlled operator runs and can be called before promotion.
func (e *Engine) GenerateShadowRound(ctx context.Context, seasonID string, round int, requested Config) (ShadowRoundReport, error) {
	if e == nil || e.DB == nil {
		return ShadowRoundReport{}, errors.New("prediction engine database is nil")
	}
	if strings.TrimSpace(seasonID) == "" || round < 1 || round > 30 {
		return ShadowRoundReport{}, errors.New("season and round are required")
	}
	cfg := requested.normalize()
	cfg.ModelName = research.DynamicBayesianModelName
	now := e.now()
	dataset, err := research.LoadDataset(ctx, e.DB)
	if err != nil {
		return ShadowRoundReport{}, fmt.Errorf("load shadow dataset: %w", err)
	}
	targets := make([]research.MatchRecord, 0, 8)
	for _, match := range dataset.Matches {
		if match.SeasonID.String() == seasonID && match.Round == round {
			targets = append(targets, match)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].MatchID < targets[j].MatchID })
	report := ShadowRoundReport{
		SeasonID:       seasonID,
		Round:          round,
		Status:         "failed",
		ModelName:      research.DynamicBayesianModelName,
		ModelVersion:   research.DynamicBayesianModelVersion,
		PredictionAsOf: now,
		FixtureCount:   len(targets),
	}
	if len(targets) != 8 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("round has %d fixtures; eight are required", len(targets)))
		return report, nil
	}
	earliest := targets[0].ScheduledAt
	for _, target := range targets[1:] {
		if target.ScheduledAt.Before(earliest) {
			earliest = target.ScheduledAt
		}
	}
	report.EarliestKickoff = earliest.UTC()
	report.CutoffAt = earliest.Add(-cfg.SafetyBuffer).UTC()
	if !now.Before(report.CutoffAt) {
		report.Warnings = append(report.Warnings, "shadow run was requested after the prediction cutoff")
		return report, nil
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
			clone.Odds = eligibleOdds(match.Odds, report.CutoffAt, cfg.RequireVerifiedOdds)
			asOfMatches = append(asOfMatches, clone)
			continue
		}
		if match.Final() && match.ScheduledAt.Before(report.CutoffAt) {
			training = append(training, match)
			clone := match
			clone.Odds = nil
			asOfMatches = append(asOfMatches, clone)
		}
	}
	inputDataset := research.NewDataset(asOfMatches, dataset.Seasons)
	report.InputVersion = inputDataset.Fingerprint()
	model := research.NewDynamicBayesianModel()
	if err := model.Fit(training); err != nil {
		return report, fmt.Errorf("fit shadow model: %w", err)
	}
	model.PrepareSeason(provider.SeasonID(seasonID))
	storage := research.NewResearchStorage(e.DB)
	modelID, err := storage.EnsureModelDefinition(ctx, research.DynamicBayesianModelName, research.DynamicBayesianModelVersion,
		"Phase 6 unified dynamic opponent-adjusted rating and regularized Bayesian attack/defense scoreline model",
		map[string]any{"phase": 6, "parameters": model.Parameters()})
	if err != nil {
		return report, err
	}
	if _, err := storage.SaveDynamicCheckpoint(ctx, modelID, seasonID, round, report.CutoffAt, report.InputVersion, model, map[string]any{
		"shadow": true, "prediction_as_of": now, "model_version": research.DynamicBayesianModelVersion,
	}); err != nil {
		return report, err
	}
	var shadowRunID, existingStatus string
	var existingGenerated int
	err = e.DB.QueryRowContext(ctx, `
INSERT INTO prediction_shadow_runs (
    season_id, round_no, model_definition_id, model_version, prediction_as_of,
    cutoff_at, input_dataset_version, status, manifest
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'generated', $8::jsonb)
ON CONFLICT (season_id, round_no, model_definition_id, model_version, input_dataset_version) DO NOTHING
RETURNING shadow_run_id::text, status, generated_fixture_count`, seasonID, round, modelID,
		research.DynamicBayesianModelVersion, now, report.CutoffAt, report.InputVersion,
		`{"prediction_policy":"dynamic_bayesian_team_strength","shadow":true}`).Scan(&shadowRunID, &existingStatus, &existingGenerated)
	if err == sql.ErrNoRows {
		err = e.DB.QueryRowContext(ctx, `
SELECT shadow_run_id::text, status, generated_fixture_count
FROM prediction_shadow_runs
WHERE season_id = $1 AND round_no = $2 AND model_definition_id = $3
  AND model_version = $4 AND input_dataset_version = $5`, seasonID, round, modelID,
			research.DynamicBayesianModelVersion, report.InputVersion).Scan(&shadowRunID, &existingStatus, &existingGenerated)
		if err == nil && existingStatus == "generated" && existingGenerated == 8 {
			report.ShadowRunID, report.Status, report.GeneratedCount, report.ExistingRun = shadowRunID, existingStatus, existingGenerated, true
			return report, nil
		}
	}
	if err != nil {
		return report, fmt.Errorf("create shadow run: %w", err)
	}
	for _, target := range targets {
		clone := target
		clone.HomeScore = nil
		clone.AwayScore = nil
		clone.Odds = eligibleOdds(target.Odds, report.CutoffAt, cfg.RequireVerifiedOdds)
		goals := model.PredictGoals(clone, model.Config.MaxScore)
		payload, marshalErr := json.Marshal(map[string]any{
			"model":                 research.DynamicBayesianModelName,
			"model_version":         research.DynamicBayesianModelVersion,
			"prediction_policy":     research.DynamicBayesianModelName,
			"secondary":             goals,
			"dynamic_team_strength": model.Diagnostics(clone),
			"input_version":         report.InputVersion,
		})
		if marshalErr != nil {
			return report, marshalErr
		}
		_, err = e.DB.ExecContext(ctx, `
INSERT INTO prediction_shadow_revisions (
    shadow_run_id, match_id, home_probability, draw_probability,
    away_probability, probability_payload
) VALUES ($1::uuid, $2, $3, $4, $5, $6::jsonb)
ON CONFLICT (shadow_run_id, match_id) DO NOTHING`, shadowRunID, target.MatchID,
			goals.Probability.Home, goals.Probability.Draw, goals.Probability.Away, string(payload))
		if err != nil {
			return report, fmt.Errorf("insert shadow revision %s: %w", target.MatchID, err)
		}
	}
	if _, err := e.DB.ExecContext(ctx, `UPDATE prediction_shadow_runs SET generated_fixture_count = 8, status = 'generated' WHERE shadow_run_id = $1::uuid`, shadowRunID); err != nil {
		return report, err
	}
	report.ShadowRunID, report.Status, report.GeneratedCount = shadowRunID, "generated", 8
	return report, nil
}
