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

type BacktestConfig struct {
	MinTrainingSeasons     int
	RoundSafetyBuffer      time.Duration
	AllowRetrospectiveOdds bool
	Models                 []string
	Seed                   int64
	BootstrapReplicates    int
}

func DefaultBacktestConfig() BacktestConfig {
	return BacktestConfig{
		MinTrainingSeasons:  1,
		RoundSafetyBuffer:   30 * time.Second,
		Seed:                20261025,
		BootstrapReplicates: 400,
		Models:              BaselineModelNames(),
	}
}

type BacktestPrediction struct {
	ModelName               string            `json:"model_name"`
	ModelVersion            string            `json:"model_version"`
	SeasonID                provider.SeasonID `json:"season_id"`
	MatchID                 provider.MatchID  `json:"match_id"`
	Round                   int               `json:"round"`
	PredictionAsOf          time.Time         `json:"prediction_as_of"`
	CutoffAt                time.Time         `json:"cutoff_at"`
	Probability             Probability       `json:"probability"`
	Actual                  OutcomeKey        `json:"actual"`
	Odds                    *OddsLine         `json:"odds,omitempty"`
	OddsPointInTimeVerified bool              `json:"odds_point_in_time_verified"`
	OddsTrack               string            `json:"odds_track"`
}

type Metrics struct {
	Count    int     `json:"count"`
	LogLoss  float64 `json:"log_loss"`
	Brier    float64 `json:"multiclass_brier"`
	Accuracy float64 `json:"accuracy"`
	ECE      float64 `json:"expected_calibration_error"`
}

type ReliabilityPoint struct {
	Class             OutcomeKey `json:"class"`
	Bin               int        `json:"bin"`
	Count             int        `json:"count"`
	MeanPredicted     float64    `json:"mean_predicted"`
	ObservedFrequency float64    `json:"observed_frequency"`
}

type ConfidenceInterval struct {
	Low        float64 `json:"low"`
	High       float64 `json:"high"`
	Replicates int     `json:"replicates"`
}

type ModelEvaluation struct {
	ModelName   string                        `json:"model_name"`
	Metrics     Metrics                       `json:"metrics"`
	BySeason    map[string]Metrics            `json:"by_season"`
	ByRound     map[string]Metrics            `json:"by_round"`
	Reliability []ReliabilityPoint            `json:"reliability"`
	Confidence  map[string]ConfidenceInterval `json:"confidence_intervals"`
	OddsTrack   string                        `json:"odds_track"`
}

type BacktestResult struct {
	DatasetFingerprint string               `json:"dataset_fingerprint"`
	TrainingPolicy     string               `json:"training_policy"`
	Predictions        []BacktestPrediction `json:"predictions"`
	Evaluations        []ModelEvaluation    `json:"evaluations"`
	SeasonsEvaluated   []provider.SeasonID  `json:"seasons_evaluated"`
	SkippedNoHistory   []provider.SeasonID  `json:"skipped_no_history"`
}

// RunBacktest performs a season-chronological, round-by-round walk-forward
// evaluation. At each target round the model is refit from final results with
// scheduled times strictly before that round's cutoff.
func RunBacktest(dataset Dataset, config BacktestConfig) (BacktestResult, error) {
	config = mergeBacktestConfig(DefaultBacktestConfig(), config)
	seasons := chronologicalSeasons(dataset)
	if len(seasons) == 0 {
		return BacktestResult{}, fmt.Errorf("dataset has no seasons")
	}
	result := BacktestResult{
		DatasetFingerprint: dataset.Fingerprint(),
		TrainingPolicy:     "walk_forward_by_season_and_round; final results strictly before cutoff",
		Predictions:        make([]BacktestPrediction, 0),
		Evaluations:        make([]ModelEvaluation, 0),
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
		rounds := roundNumbers(targets)
		for _, round := range rounds {
			roundTargets := filterRound(targets, round)
			cutoff := roundCutoff(roundTargets, config.RoundSafetyBuffer)
			training := matchesBefore(dataset.Matches, cutoff)
			models := make(map[string]BaselineModel, len(config.Models))
			for _, modelName := range config.Models {
				model, err := NewBaselineModel(modelName, config.AllowRetrospectiveOdds)
				if err != nil {
					return BacktestResult{}, err
				}
				if err := model.Fit(training); err != nil {
					return BacktestResult{}, fmt.Errorf("fit %s at %s: %w", modelName, cutoff.Format(time.RFC3339), err)
				}
				models[modelName] = model
			}
			for _, target := range roundTargets {
				actual, ok := target.Outcome()
				if !ok {
					continue
				}
				asOfTarget := target
				asOfTarget.Odds = oddsAtBacktestCutoff(target.Odds, cutoff, config.AllowRetrospectiveOdds)
				oddsLine, oddsAvailable := backtestOddsLine(asOfTarget, cutoff, config.AllowRetrospectiveOdds)
				for _, modelName := range config.Models {
					if modelName == "bookmaker_implied" {
						if !oddsAvailable {
							continue
						}
					}
					model := models[modelName]
					probability := model.Predict(asOfTarget)
					if err := probability.Validate(1e-8); err != nil {
						return BacktestResult{}, fmt.Errorf("model %s produced invalid probability for match %s: %w", modelName, target.MatchID, err)
					}
					prediction := BacktestPrediction{
						ModelName:      modelName,
						ModelVersion:   model.Version(),
						SeasonID:       target.SeasonID,
						MatchID:        target.MatchID,
						Round:          target.Round,
						PredictionAsOf: cutoff.UTC(),
						CutoffAt:       cutoff.UTC(),
						Probability:    probability,
						Actual:         actual,
						OddsTrack:      "none",
					}
					if oddsAvailable {
						lineCopy := oddsLine
						prediction.Odds = &lineCopy
						prediction.OddsPointInTimeVerified = oddsLine.PointInTimeVerified
						if oddsLine.PointInTimeVerified {
							prediction.OddsTrack = "verified_point_in_time"
						} else {
							prediction.OddsTrack = "retrospective_unverified_availability"
						}
					}
					result.Predictions = append(result.Predictions, prediction)
				}
			}
		}
	}
	for _, modelName := range config.Models {
		predictions := filterModelPredictions(result.Predictions, modelName)
		if len(predictions) == 0 {
			continue
		}
		evaluation := evaluateModel(modelName, predictions, config.Seed, config.BootstrapReplicates)
		result.Evaluations = append(result.Evaluations, evaluation)
	}
	return result, nil
}

func oddsAtBacktestCutoff(quotes []OddsQuote, cutoff time.Time, allowRetrospective bool) []OddsQuote {
	if allowRetrospective {
		return append([]OddsQuote(nil), quotes...)
	}
	filtered := make([]OddsQuote, 0, len(quotes))
	for _, quote := range quotes {
		if quote.PointInTimeVerified && !quote.ObservedAt.IsZero() && !quote.ObservedAt.After(cutoff) {
			filtered = append(filtered, quote)
		}
	}
	return filtered
}

func backtestOddsLine(match MatchRecord, cutoff time.Time, allowRetrospective bool) (OddsLine, bool) {
	if allowRetrospective {
		return FindOneXTwoOdds(match, true)
	}
	return FindOneXTwoOddsAt(match, cutoff)
}

func mergeBacktestConfig(defaults, provided BacktestConfig) BacktestConfig {
	if provided.MinTrainingSeasons != 0 {
		defaults.MinTrainingSeasons = provided.MinTrainingSeasons
	}
	if provided.RoundSafetyBuffer != 0 {
		defaults.RoundSafetyBuffer = provided.RoundSafetyBuffer
	}
	defaults.AllowRetrospectiveOdds = provided.AllowRetrospectiveOdds
	if len(provided.Models) > 0 {
		defaults.Models = append([]string(nil), provided.Models...)
	}
	if provided.Seed != 0 {
		defaults.Seed = provided.Seed
	}
	if provided.BootstrapReplicates != 0 {
		defaults.BootstrapReplicates = provided.BootstrapReplicates
	}
	return defaults
}

func chronologicalSeasons(dataset Dataset) []SeasonRecord {
	seasons := make(map[provider.SeasonID]SeasonRecord)
	seasonHasMatches := make(map[provider.SeasonID]bool)
	for _, match := range dataset.Matches {
		seasonHasMatches[match.SeasonID] = true
		if _, exists := seasons[match.SeasonID]; !exists {
			seasons[match.SeasonID] = SeasonRecord{ID: match.SeasonID, StartUTC: match.ScheduledAt, EndUTC: match.ScheduledAt}
		}
	}
	for _, season := range dataset.Seasons {
		if seasonHasMatches[season.ID] {
			seasons[season.ID] = season
		}
	}
	result := make([]SeasonRecord, 0, len(seasons))
	for _, season := range seasons {
		result = append(result, season)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].StartUTC.Equal(result[j].StartUTC) {
			return result[i].StartUTC.Before(result[j].StartUTC)
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func matchesForSeason(matches []MatchRecord, seasonID provider.SeasonID) []MatchRecord {
	result := make([]MatchRecord, 0)
	for _, match := range matches {
		if match.SeasonID == seasonID {
			result = append(result, match)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].ScheduledAt.Equal(result[j].ScheduledAt) {
			return result[i].ScheduledAt.Before(result[j].ScheduledAt)
		}
		return result[i].MatchID < result[j].MatchID
	})
	return result
}

func roundNumbers(matches []MatchRecord) []int {
	seen := make(map[int]struct{})
	for _, match := range matches {
		seen[match.Round] = struct{}{}
	}
	result := make([]int, 0, len(seen))
	for round := range seen {
		result = append(result, round)
	}
	sort.Ints(result)
	return result
}

func filterRound(matches []MatchRecord, round int) []MatchRecord {
	result := make([]MatchRecord, 0)
	for _, match := range matches {
		if match.Round == round {
			result = append(result, match)
		}
	}
	return result
}

func roundCutoff(matches []MatchRecord, safetyBuffer time.Duration) time.Time {
	var earliest time.Time
	for _, match := range matches {
		if earliest.IsZero() || match.ScheduledAt.Before(earliest) {
			earliest = match.ScheduledAt
		}
	}
	if safetyBuffer < 0 {
		safetyBuffer = 0
	}
	return earliest.Add(-safetyBuffer).UTC()
}

func matchesBefore(matches []MatchRecord, cutoff time.Time) []MatchRecord {
	result := make([]MatchRecord, 0)
	for _, match := range matches {
		if match.Final() && match.ScheduledAt.Before(cutoff) {
			result = append(result, match)
		}
	}
	return result
}

func filterModelPredictions(predictions []BacktestPrediction, modelName string) []BacktestPrediction {
	result := make([]BacktestPrediction, 0)
	for _, prediction := range predictions {
		if prediction.ModelName == modelName {
			result = append(result, prediction)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].PredictionAsOf.Equal(result[j].PredictionAsOf) {
			return result[i].PredictionAsOf.Before(result[j].PredictionAsOf)
		}
		return result[i].MatchID < result[j].MatchID
	})
	return result
}

func evaluateModel(modelName string, predictions []BacktestPrediction, seed int64, replicates int) ModelEvaluation {
	metrics := ComputeMetrics(predictions)
	bySeason := make(map[string]Metrics)
	byRound := make(map[string]Metrics)
	seasons := make(map[string][]BacktestPrediction)
	rounds := make(map[int][]BacktestPrediction)
	track := "none"
	for _, prediction := range predictions {
		seasons[prediction.SeasonID.String()] = append(seasons[prediction.SeasonID.String()], prediction)
		rounds[prediction.Round] = append(rounds[prediction.Round], prediction)
		if prediction.OddsTrack != "none" {
			track = prediction.OddsTrack
		}
	}
	for season, values := range seasons {
		bySeason[season] = ComputeMetrics(values)
	}
	for round, values := range rounds {
		byRound[strconv.Itoa(round)] = ComputeMetrics(values)
	}
	return ModelEvaluation{
		ModelName:   modelName,
		Metrics:     metrics,
		BySeason:    bySeason,
		ByRound:     byRound,
		Reliability: Reliability(predictions, 10),
		Confidence:  BlockBootstrapConfidence(predictions, seed, replicates),
		OddsTrack:   track,
	}
}

func ComputeMetrics(predictions []BacktestPrediction) Metrics {
	if len(predictions) == 0 {
		return Metrics{}
	}
	logLoss, brier, correct := 0.0, 0.0, 0
	for _, prediction := range predictions {
		probability := clamp(prediction.Probability.At(prediction.Actual), 1e-15, 1)
		logLoss -= math.Log(probability)
		for _, outcome := range []OutcomeKey{OutcomeHome, OutcomeDraw, OutcomeAway} {
			observed := 0.0
			if prediction.Actual == outcome {
				observed = 1
			}
			difference := prediction.Probability.At(outcome) - observed
			brier += difference * difference
		}
		if argmaxOutcome(prediction.Probability) == prediction.Actual {
			correct++
		}
	}
	count := float64(len(predictions))
	return Metrics{
		Count:    len(predictions),
		LogLoss:  logLoss / count,
		Brier:    brier / count,
		Accuracy: float64(correct) / count,
		ECE:      expectedCalibrationError(predictions, 10),
	}
}

func Reliability(predictions []BacktestPrediction, bins int) []ReliabilityPoint {
	if bins <= 0 {
		bins = 10
	}
	result := make([]ReliabilityPoint, 0, bins*3)
	for _, outcome := range []OutcomeKey{OutcomeHome, OutcomeDraw, OutcomeAway} {
		for bin := 0; bin < bins; bin++ {
			count := 0
			predicted, observed := 0.0, 0.0
			for _, prediction := range predictions {
				value := prediction.Probability.At(outcome)
				index := int(math.Floor(value * float64(bins)))
				if index >= bins {
					index = bins - 1
				}
				if index != bin {
					continue
				}
				count++
				predicted += value
				if prediction.Actual == outcome {
					observed++
				}
			}
			if count > 0 {
				result = append(result, ReliabilityPoint{
					Class:             outcome,
					Bin:               bin,
					Count:             count,
					MeanPredicted:     predicted / float64(count),
					ObservedFrequency: observed / float64(count),
				})
			}
		}
	}
	return result
}

func expectedCalibrationError(predictions []BacktestPrediction, bins int) float64 {
	reliability := Reliability(predictions, bins)
	if len(predictions) == 0 {
		return 0
	}
	weighted := 0.0
	for _, point := range reliability {
		weighted += float64(point.Count) * math.Abs(point.MeanPredicted-point.ObservedFrequency)
	}
	return weighted / float64(len(predictions)*3)
}

func BlockBootstrapConfidence(predictions []BacktestPrediction, seed int64, replicates int) map[string]ConfidenceInterval {
	result := make(map[string]ConfidenceInterval)
	if len(predictions) == 0 || replicates <= 0 {
		return result
	}
	blocksMap := make(map[string][]BacktestPrediction)
	for _, prediction := range predictions {
		blocksMap[prediction.SeasonID.String()] = append(blocksMap[prediction.SeasonID.String()], prediction)
	}
	blocks := make([][]BacktestPrediction, 0, len(blocksMap))
	keys := make([]string, 0, len(blocksMap))
	for key := range blocksMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		blocks = append(blocks, blocksMap[key])
	}
	rng := rand.New(rand.NewSource(seed))
	values := map[string][]float64{"log_loss": {}, "brier": {}, "accuracy": {}}
	for replicate := 0; replicate < replicates; replicate++ {
		sample := make([]BacktestPrediction, 0, len(predictions))
		for index := 0; index < len(blocks); index++ {
			block := blocks[rng.Intn(len(blocks))]
			sample = append(sample, block...)
		}
		metrics := ComputeMetrics(sample)
		values["log_loss"] = append(values["log_loss"], metrics.LogLoss)
		values["brier"] = append(values["brier"], metrics.Brier)
		values["accuracy"] = append(values["accuracy"], metrics.Accuracy)
	}
	for key, samples := range values {
		sort.Float64s(samples)
		result[key] = ConfidenceInterval{Low: quantile(samples, 0.025), High: quantile(samples, 0.975), Replicates: len(samples)}
	}
	return result
}

func quantile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if fraction <= 0 {
		return values[0]
	}
	if fraction >= 1 {
		return values[len(values)-1]
	}
	index := int(math.Round(fraction * float64(len(values)-1)))
	return values[index]
}

func argmaxOutcome(probability Probability) OutcomeKey {
	if probability.Home >= probability.Draw && probability.Home >= probability.Away {
		return OutcomeHome
	}
	if probability.Draw >= probability.Away {
		return OutcomeDraw
	}
	return OutcomeAway
}
