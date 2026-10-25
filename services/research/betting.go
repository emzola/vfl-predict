package research

import (
	"sort"
	"time"
)

type BettingConfig struct {
	MinimumExpectedValue   float64
	Stake                  float64
	AllowRetrospectiveOdds bool
	OneSelectionPerMatch   bool
}

func DefaultBettingConfig() BettingConfig {
	return BettingConfig{
		MinimumExpectedValue:   0,
		Stake:                  1,
		AllowRetrospectiveOdds: false,
		OneSelectionPerMatch:   true,
	}
}

type BettingSelection struct {
	ModelName             string     `json:"model_name"`
	SeasonID              string     `json:"season_id"`
	MatchID               string     `json:"match_id"`
	Round                 int        `json:"round"`
	Outcome               OutcomeKey `json:"outcome"`
	ModelProbability      float64    `json:"model_probability"`
	QuotedDecimalOdds     float64    `json:"quoted_decimal_odds"`
	BookmakerImplied      float64    `json:"bookmaker_implied_probability"`
	ExpectedValue         float64    `json:"expected_value"`
	ProbabilityDifference float64    `json:"probability_difference"`
	Stake                 float64    `json:"stake"`
	RealizedReturn        float64    `json:"realized_return"`
	RealizedProfit        float64    `json:"realized_profit"`
	PredictionAsOf        time.Time  `json:"prediction_as_of"`
	OddsObservedAt        time.Time  `json:"odds_observed_at"`
	PointInTimeVerified   bool       `json:"point_in_time_verified"`
	OddsTrack             string     `json:"odds_track"`
}

type BettingMetrics struct {
	Selections      int     `json:"selections"`
	HitRate         float64 `json:"hit_rate"`
	TotalStake      float64 `json:"total_stake"`
	TotalReturn     float64 `json:"total_return"`
	Profit          float64 `json:"profit"`
	ROI             float64 `json:"roi"`
	MaximumDrawdown float64 `json:"maximum_drawdown"`
}

type BettingSeasonSummary struct {
	SeasonID string         `json:"season_id"`
	Metrics  BettingMetrics `json:"metrics"`
}

type BettingReport struct {
	MinimumExpectedValue   float64                `json:"minimum_expected_value"`
	Stake                  float64                `json:"stake"`
	AllowRetrospectiveOdds bool                   `json:"allow_retrospective_odds"`
	OddsTrack              string                 `json:"odds_track"`
	SkippedUnverified      int                    `json:"skipped_unverified"`
	Selections             []BettingSelection     `json:"selections"`
	Aggregate              BettingMetrics         `json:"aggregate"`
	BySeason               []BettingSeasonSummary `json:"by_season"`
}

// AnalyzeBetting is explicitly a research calculation. It does not place
// wagers, maintain a bankroll, or imply that retrospective odds were
// available at PredictionAsOf.
func AnalyzeBetting(predictions []BacktestPrediction, config BettingConfig) BettingReport {
	if config.Stake <= 0 {
		config.Stake = 1
	}
	result := BettingReport{
		MinimumExpectedValue:   config.MinimumExpectedValue,
		Stake:                  config.Stake,
		AllowRetrospectiveOdds: config.AllowRetrospectiveOdds,
		OddsTrack:              "verified_point_in_time",
		Selections:             make([]BettingSelection, 0),
	}
	ordered := append([]BacktestPrediction(nil), predictions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].PredictionAsOf.Equal(ordered[j].PredictionAsOf) {
			return ordered[i].PredictionAsOf.Before(ordered[j].PredictionAsOf)
		}
		if ordered[i].SeasonID != ordered[j].SeasonID {
			return ordered[i].SeasonID < ordered[j].SeasonID
		}
		return ordered[i].MatchID < ordered[j].MatchID
	})
	seenMatches := make(map[string]struct{})
	for _, prediction := range ordered {
		if prediction.Odds == nil {
			continue
		}
		line := prediction.Odds
		if !line.PointInTimeVerified {
			result.SkippedUnverified++
			if !config.AllowRetrospectiveOdds {
				continue
			}
			result.OddsTrack = "retrospective_unverified_availability"
		}
		matchKey := prediction.SeasonID.String() + "/" + prediction.MatchID.String()
		if config.OneSelectionPerMatch {
			if _, exists := seenMatches[matchKey]; exists {
				continue
			}
		}
		outcomes := []struct {
			key  OutcomeKey
			odd  float64
			when time.Time
		}{
			{OutcomeHome, line.Home, line.HomeQuote.ObservedAt},
			{OutcomeDraw, line.Draw, line.DrawQuote.ObservedAt},
			{OutcomeAway, line.Away, line.AwayQuote.ObservedAt},
		}
		bestIndex := -1
		bestEV := config.MinimumExpectedValue
		for index, outcome := range outcomes {
			modelProbability := prediction.Probability.At(outcome.key)
			ev := modelProbability*outcome.odd - 1
			if ev >= bestEV {
				bestEV = ev
				bestIndex = index
			}
		}
		if bestIndex < 0 {
			continue
		}
		selected := outcomes[bestIndex]
		modelProbability := prediction.Probability.At(selected.key)
		implied := 1 / selected.odd
		profit := -config.Stake
		realizedReturn := 0.0
		if prediction.Actual == selected.key {
			realizedReturn = config.Stake * selected.odd
			profit = realizedReturn - config.Stake
		}
		result.Selections = append(result.Selections, BettingSelection{
			ModelName:             prediction.ModelName,
			SeasonID:              prediction.SeasonID.String(),
			MatchID:               prediction.MatchID.String(),
			Round:                 prediction.Round,
			Outcome:               selected.key,
			ModelProbability:      modelProbability,
			QuotedDecimalOdds:     selected.odd,
			BookmakerImplied:      implied,
			ExpectedValue:         modelProbability*selected.odd - 1,
			ProbabilityDifference: modelProbability - implied,
			Stake:                 config.Stake,
			RealizedReturn:        realizedReturn,
			RealizedProfit:        profit,
			PredictionAsOf:        prediction.PredictionAsOf,
			OddsObservedAt:        selected.when,
			PointInTimeVerified:   line.PointInTimeVerified,
			OddsTrack:             prediction.OddsTrack,
		})
		seenMatches[matchKey] = struct{}{}
	}
	result.Aggregate = bettingMetrics(result.Selections)
	bySeason := make(map[string][]BettingSelection)
	for _, selection := range result.Selections {
		bySeason[selection.SeasonID] = append(bySeason[selection.SeasonID], selection)
	}
	seasonIDs := make([]string, 0, len(bySeason))
	for seasonID := range bySeason {
		seasonIDs = append(seasonIDs, seasonID)
	}
	sort.Strings(seasonIDs)
	for _, seasonID := range seasonIDs {
		result.BySeason = append(result.BySeason, BettingSeasonSummary{SeasonID: seasonID, Metrics: bettingMetrics(bySeason[seasonID])})
	}
	return result
}

func bettingMetrics(selections []BettingSelection) BettingMetrics {
	result := BettingMetrics{Selections: len(selections)}
	if len(selections) == 0 {
		return result
	}
	for _, selection := range selections {
		result.TotalStake += selection.Stake
		result.TotalReturn += selection.RealizedReturn
		if selection.RealizedReturn > 0 {
			result.HitRate++
		}
	}
	result.Profit = result.TotalReturn - result.TotalStake
	if result.TotalStake > 0 {
		result.ROI = result.Profit / result.TotalStake
	}
	result.HitRate /= float64(len(selections))
	cumulative, peak, drawdown := 0.0, 0.0, 0.0
	for _, selection := range selections {
		cumulative += selection.RealizedProfit
		if cumulative > peak {
			peak = cumulative
		}
		if current := peak - cumulative; current > drawdown {
			drawdown = current
		}
	}
	result.MaximumDrawdown = drawdown
	return result
}
