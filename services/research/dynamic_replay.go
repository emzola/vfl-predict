package research

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"bet9ja-vfl/services/provider"
)

// DynamicReplayConfig controls the deterministic, season-aware Phase 6
// replay. The model is refit at every target round using only final results
// strictly before that round's cutoff.
type DynamicReplayConfig struct {
	MinTrainingSeasons  int
	RoundSafetyBuffer   time.Duration
	ModelConfig         DynamicBayesianConfig
	Seed                int64
	BootstrapReplicates int
	MaxScore            int
}

func DefaultDynamicReplayConfig() DynamicReplayConfig {
	return DynamicReplayConfig{
		MinTrainingSeasons:  1,
		RoundSafetyBuffer:   30 * time.Second,
		ModelConfig:         DefaultDynamicBayesianConfig(),
		Seed:                20261026,
		BootstrapReplicates: 400,
		MaxScore:            12,
	}
}

type BinaryMarketMetrics struct {
	Count    int     `json:"count"`
	LogLoss  float64 `json:"log_loss"`
	Brier    float64 `json:"brier"`
	Accuracy float64 `json:"accuracy"`
}

type DynamicReplayMetrics struct {
	OneXTwo             Metrics             `json:"one_x_two"`
	BTTS                BinaryMarketMetrics `json:"btts"`
	OverUnder25         BinaryMarketMetrics `json:"over_under_2_5"`
	CorrectScoreLogLoss float64             `json:"correct_score_log_loss"`
	CorrectScoreCount   int                 `json:"correct_score_count"`
}

type DynamicReplayPrediction struct {
	ModelName               string                     `json:"model_name"`
	ModelVersion            string                     `json:"model_version"`
	SeasonID                provider.SeasonID          `json:"season_id"`
	MatchID                 provider.MatchID           `json:"match_id"`
	Round                   int                        `json:"round"`
	PredictionAsOf          time.Time                  `json:"prediction_as_of"`
	CutoffAt                time.Time                  `json:"cutoff_at"`
	Probability             Probability                `json:"probability"`
	ExpectedHomeGoals       float64                    `json:"expected_home_goals"`
	ExpectedAwayGoals       float64                    `json:"expected_away_goals"`
	BTTSYesProbability      float64                    `json:"btts_yes_probability"`
	BTTSNoProbability       float64                    `json:"btts_no_probability"`
	Over25Probability       float64                    `json:"over_2_5_probability"`
	Under25Probability      float64                    `json:"under_2_5_probability"`
	CorrectScoreProbability float64                    `json:"correct_score_probability"`
	ActualScore             string                     `json:"actual_score"`
	ActualOutcome           OutcomeKey                 `json:"actual_outcome"`
	Diagnostics             DynamicBayesianDiagnostics `json:"diagnostics"`
}

type DynamicReplayEvaluation struct {
	ModelName    string                          `json:"model_name"`
	ModelVersion string                          `json:"model_version"`
	Metrics      DynamicReplayMetrics            `json:"metrics"`
	BySeason     map[string]DynamicReplayMetrics `json:"by_season"`
	ByRound      map[string]DynamicReplayMetrics `json:"by_round"`
	Confidence   map[string]ConfidenceInterval   `json:"confidence_intervals"`
	OddsTrack    string                          `json:"odds_track"`
}

type DynamicReplayResult struct {
	DatasetFingerprint string                    `json:"dataset_fingerprint"`
	TrainingPolicy     string                    `json:"training_policy"`
	ModelName          string                    `json:"model_name"`
	ModelVersion       string                    `json:"model_version"`
	Predictions        []DynamicReplayPrediction `json:"predictions"`
	Evaluation         DynamicReplayEvaluation   `json:"evaluation"`
	SeasonsEvaluated   []provider.SeasonID       `json:"seasons_evaluated"`
	SkippedNoHistory   []provider.SeasonID       `json:"skipped_no_history"`
}

// RunDynamicBayesianReplay performs a chronological walk-forward replay. It
// intentionally does not write production prediction_runs or revisions.
func RunDynamicBayesianReplay(dataset Dataset, provided DynamicReplayConfig) (DynamicReplayResult, error) {
	config := mergeDynamicReplayConfig(DefaultDynamicReplayConfig(), provided)
	seasons := chronologicalSeasons(dataset)
	if len(seasons) == 0 {
		return DynamicReplayResult{}, fmt.Errorf("dataset has no seasons")
	}
	result := DynamicReplayResult{
		DatasetFingerprint: dataset.Fingerprint(),
		TrainingPolicy:     "phase6_walk_forward_by_season_and_round; final results strictly before cutoff; dynamic state reset with controlled carryover",
		ModelName:          DynamicBayesianModelName,
		ModelVersion:       DynamicBayesianModelVersion,
		Predictions:        make([]DynamicReplayPrediction, 0),
		SeasonsEvaluated:   make([]provider.SeasonID, 0),
		SkippedNoHistory:   make([]provider.SeasonID, 0),
	}
	if config.MinTrainingSeasons < 1 {
		config.MinTrainingSeasons = 1
	}
	for seasonIndex, season := range seasons {
		if seasonIndex < config.MinTrainingSeasons {
			result.SkippedNoHistory = append(result.SkippedNoHistory, season.ID)
			continue
		}
		targets := matchesForSeason(dataset.Matches, season.ID)
		if len(targets) == 0 {
			continue
		}
		result.SeasonsEvaluated = append(result.SeasonsEvaluated, season.ID)
		for _, round := range roundNumbers(targets) {
			roundTargets := filterRound(targets, round)
			cutoff := roundCutoff(roundTargets, config.RoundSafetyBuffer)
			training := matchesBefore(dataset.Matches, cutoff)
			model := NewDynamicBayesianModel()
			model.Config = config.ModelConfig
			if err := model.Fit(training); err != nil {
				return DynamicReplayResult{}, fmt.Errorf("fit dynamic model at %s: %w", cutoff.Format(time.RFC3339), err)
			}
			model.PrepareSeason(season.ID)
			for _, target := range roundTargets {
				actual, ok := target.Outcome()
				if !ok {
					continue
				}
				goals := model.PredictGoals(target, config.MaxScore)
				if err := goals.Probability.Validate(1e-8); err != nil {
					return DynamicReplayResult{}, fmt.Errorf("invalid dynamic probability for match %s: %w", target.MatchID, err)
				}
				correctScore := fmt.Sprintf("%d-%d", *target.HomeScore, *target.AwayScore)
				result.Predictions = append(result.Predictions, DynamicReplayPrediction{
					ModelName:               DynamicBayesianModelName,
					ModelVersion:            DynamicBayesianModelVersion,
					SeasonID:                target.SeasonID,
					MatchID:                 target.MatchID,
					Round:                   target.Round,
					PredictionAsOf:          cutoff.UTC(),
					CutoffAt:                cutoff.UTC(),
					Probability:             goals.Probability,
					ExpectedHomeGoals:       goals.ExpectedHomeGoals,
					ExpectedAwayGoals:       goals.ExpectedAwayGoals,
					BTTSYesProbability:      goals.BothTeamsToScore,
					BTTSNoProbability:       goals.NeitherTeamScores,
					Over25Probability:       goals.Over25,
					Under25Probability:      goals.Under25,
					CorrectScoreProbability: clamp(goals.CorrectScores[correctScore], 1e-15, 1),
					ActualScore:             correctScore,
					ActualOutcome:           actual,
					Diagnostics:             model.Diagnostics(target),
				})
			}
		}
	}
	result.Evaluation = evaluateDynamicReplay(result.Predictions, config.Seed, config.BootstrapReplicates)
	return result, nil
}

func mergeDynamicReplayConfig(defaults, provided DynamicReplayConfig) DynamicReplayConfig {
	if provided.MinTrainingSeasons != 0 {
		defaults.MinTrainingSeasons = provided.MinTrainingSeasons
	}
	if provided.RoundSafetyBuffer != 0 {
		defaults.RoundSafetyBuffer = provided.RoundSafetyBuffer
	}
	if provided.ModelConfig != (DynamicBayesianConfig{}) {
		defaults.ModelConfig = provided.ModelConfig
	}
	if provided.Seed != 0 {
		defaults.Seed = provided.Seed
	}
	if provided.BootstrapReplicates != 0 {
		defaults.BootstrapReplicates = provided.BootstrapReplicates
	}
	if provided.MaxScore != 0 {
		defaults.MaxScore = provided.MaxScore
	}
	return defaults
}

func evaluateDynamicReplay(predictions []DynamicReplayPrediction, seed int64, replicates int) DynamicReplayEvaluation {
	evaluation := DynamicReplayEvaluation{
		ModelName:    DynamicBayesianModelName,
		ModelVersion: DynamicBayesianModelVersion,
		BySeason:     make(map[string]DynamicReplayMetrics),
		ByRound:      make(map[string]DynamicReplayMetrics),
		Confidence:   make(map[string]ConfidenceInterval),
		OddsTrack:    "not used; retrospective odds are excluded from this replay",
	}
	backtest := make([]BacktestPrediction, 0, len(predictions))
	for _, prediction := range predictions {
		backtest = append(backtest, BacktestPrediction{
			ModelName:      prediction.ModelName,
			ModelVersion:   prediction.ModelVersion,
			SeasonID:       prediction.SeasonID,
			MatchID:        prediction.MatchID,
			Round:          prediction.Round,
			PredictionAsOf: prediction.PredictionAsOf,
			CutoffAt:       prediction.CutoffAt,
			Probability:    prediction.Probability,
			Actual:         prediction.ActualOutcome,
			OddsTrack:      "none",
		})
	}
	evaluation.Metrics.OneXTwo = ComputeMetrics(backtest)
	evaluation.Metrics.BTTS, evaluation.Metrics.OverUnder25, evaluation.Metrics.CorrectScoreLogLoss = goalMetrics(predictions)
	for season, values := range dynamicReplayBlocks(predictions, func(value DynamicReplayPrediction) string { return value.SeasonID.String() }) {
		evaluation.BySeason[season] = dynamicReplaySliceMetrics(values)
	}
	for round, values := range dynamicReplayBlocks(predictions, func(value DynamicReplayPrediction) string { return strconv.Itoa(value.Round) }) {
		evaluation.ByRound[round] = dynamicReplaySliceMetrics(values)
	}
	confidence := BlockBootstrapConfidence(backtest, seed, replicates)
	for key, value := range confidence {
		evaluation.Confidence[key] = value
	}
	for key, value := range dynamicGoalConfidence(predictions, seed+17, replicates) {
		evaluation.Confidence[key] = value
	}
	evaluation.Metrics.CorrectScoreCount = len(predictions)
	evaluation.Metrics.CorrectScoreLogLoss = correctScoreLogLoss(predictions)
	return evaluation
}

func dynamicReplaySliceMetrics(predictions []DynamicReplayPrediction) DynamicReplayMetrics {
	backtest := make([]BacktestPrediction, 0, len(predictions))
	for _, prediction := range predictions {
		backtest = append(backtest, BacktestPrediction{Probability: prediction.Probability, Actual: prediction.ActualOutcome, SeasonID: prediction.SeasonID, MatchID: prediction.MatchID, Round: prediction.Round})
	}
	btts, over, correct := goalMetrics(predictions)
	return DynamicReplayMetrics{
		OneXTwo:             ComputeMetrics(backtest),
		BTTS:                btts,
		OverUnder25:         over,
		CorrectScoreLogLoss: correct,
		CorrectScoreCount:   len(predictions),
	}
}

func dynamicReplayBlocks(predictions []DynamicReplayPrediction, key func(DynamicReplayPrediction) string) map[string][]DynamicReplayPrediction {
	blocks := make(map[string][]DynamicReplayPrediction)
	for _, prediction := range predictions {
		blockKey := key(prediction)
		blocks[blockKey] = append(blocks[blockKey], prediction)
	}
	return blocks
}

func goalMetrics(predictions []DynamicReplayPrediction) (BinaryMarketMetrics, BinaryMarketMetrics, float64) {
	btts := BinaryMarketMetrics{}
	over := BinaryMarketMetrics{}
	correctLoss := 0.0
	for _, prediction := range predictions {
		home, away := 0, 0
		if _, err := fmt.Sscanf(prediction.ActualScore, "%d-%d", &home, &away); err != nil {
			continue
		}
		btts = addBinaryMetric(btts, prediction.BTTSYesProbability, home >= 1 && away >= 1)
		over = addBinaryMetric(over, prediction.Over25Probability, home+away >= 3)
		correctLoss -= math.Log(clamp(prediction.CorrectScoreProbability, 1e-15, 1))
	}
	if btts.Count > 0 {
		btts.LogLoss /= float64(btts.Count)
		btts.Brier /= float64(btts.Count)
		btts.Accuracy /= float64(btts.Count)
	}
	if over.Count > 0 {
		over.LogLoss /= float64(over.Count)
		over.Brier /= float64(over.Count)
		over.Accuracy /= float64(over.Count)
	}
	if len(predictions) > 0 {
		correctLoss /= float64(len(predictions))
	}
	return btts, over, correctLoss
}

func addBinaryMetric(metrics BinaryMarketMetrics, probability float64, observed bool) BinaryMarketMetrics {
	probability = clamp(probability, 1e-15, 1-1e-15)
	value := 0.0
	if observed {
		value = 1
	}
	metrics.Count++
	if observed {
		metrics.LogLoss -= math.Log(probability)
	} else {
		metrics.LogLoss -= math.Log(1 - probability)
	}
	difference := probability - value
	metrics.Brier += difference * difference
	if (probability >= 0.5) == observed {
		metrics.Accuracy++
	}
	return metrics
}

func correctScoreLogLoss(predictions []DynamicReplayPrediction) float64 {
	if len(predictions) == 0 {
		return 0
	}
	value := 0.0
	for _, prediction := range predictions {
		value -= math.Log(clamp(prediction.CorrectScoreProbability, 1e-15, 1))
	}
	return value / float64(len(predictions))
}

func dynamicGoalConfidence(predictions []DynamicReplayPrediction, seed int64, replicates int) map[string]ConfidenceInterval {
	result := make(map[string]ConfidenceInterval)
	if len(predictions) == 0 || replicates <= 0 {
		return result
	}
	blocksMap := dynamicReplayBlocks(predictions, func(value DynamicReplayPrediction) string { return value.SeasonID.String() })
	keys := make([]string, 0, len(blocksMap))
	for key := range blocksMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	blocks := make([][]DynamicReplayPrediction, 0, len(keys))
	for _, key := range keys {
		blocks = append(blocks, blocksMap[key])
	}
	rng := rand.New(rand.NewSource(seed))
	values := map[string][]float64{"btts_log_loss": {}, "over_2_5_log_loss": {}, "correct_score_log_loss": {}}
	for replicate := 0; replicate < replicates; replicate++ {
		sample := make([]DynamicReplayPrediction, 0, len(predictions))
		for index := 0; index < len(blocks); index++ {
			sample = append(sample, blocks[rng.Intn(len(blocks))]...)
		}
		btts, over, correct := goalMetrics(sample)
		values["btts_log_loss"] = append(values["btts_log_loss"], btts.LogLoss)
		values["over_2_5_log_loss"] = append(values["over_2_5_log_loss"], over.LogLoss)
		values["correct_score_log_loss"] = append(values["correct_score_log_loss"], correct)
	}
	for key, samples := range values {
		sort.Float64s(samples)
		result[key] = ConfidenceInterval{Low: quantile(samples, 0.025), High: quantile(samples, 0.975), Replicates: len(samples)}
	}
	return result
}
