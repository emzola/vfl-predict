package main

import (
	"fmt"
	"time"

	"bet9ja-vfl/services/provider"
	"bet9ja-vfl/services/research"
)

func main() {
	dataset := verificationDataset()
	features, err := research.BuildFeatureVectors(dataset, research.DefaultFeatureConfig())
	if err != nil {
		fail(err)
	}
	if len(features) != len(dataset.Matches) {
		fail(fmt.Errorf("feature count=%d want %d", len(features), len(dataset.Matches)))
	}
	config := research.DefaultBacktestConfig()
	config.Models = research.BaselineModelNames()
	config.BootstrapReplicates = 20
	backtest, err := research.RunBacktest(dataset, config)
	if err != nil {
		fail(err)
	}
	if len(backtest.SeasonsEvaluated) == 0 || len(backtest.Predictions) == 0 {
		fail(fmt.Errorf("verification dataset did not produce chronological predictions"))
	}
	for _, prediction := range backtest.Predictions {
		if err := prediction.Probability.Validate(1e-8); err != nil {
			fail(err)
		}
	}
	selected := make([]research.BacktestPrediction, 0)
	for _, prediction := range backtest.Predictions {
		if prediction.ModelName == "statistical_bookmaker_combination" {
			selected = append(selected, prediction)
		}
	}
	betting := research.AnalyzeBetting(selected, research.DefaultBettingConfig())
	if betting.Aggregate.Selections == 0 {
		fail(fmt.Errorf("verification dataset produced no betting-analysis selections"))
	}
	futureMutationCheck(dataset)
	fmt.Printf("Phase 2 offline verification passed: seasons=%d matches=%d features=%d predictions=%d selections=%d\n",
		len(dataset.Seasons), len(dataset.Matches), len(features), len(backtest.Predictions), betting.Aggregate.Selections)
}

func verificationDataset() research.Dataset {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	seasons := []research.SeasonRecord{
		{ID: "verify-2024", StartUTC: base, EndUTC: base.AddDate(0, 0, 10)},
		{ID: "verify-2025", StartUTC: base.AddDate(1, 0, 0), EndUTC: base.AddDate(1, 0, 10)},
	}
	matches := make([]research.MatchRecord, 0, 32)
	matchID := provider.MatchID(5000)
	for seasonIndex, season := range seasons {
		for round := 1; round <= 2; round++ {
			for pairing := 0; pairing < 8; pairing++ {
				home := provider.TeamUID(pairing + 1)
				away := provider.TeamUID(pairing + 9)
				scheduled := season.StartUTC.Add(time.Duration(round-1) * 24 * time.Hour)
				observation := scheduled.Add(-time.Hour)
				homeScore := (pairing + round + seasonIndex) % 3
				awayScore := (pairing + seasonIndex) % 2
				matches = append(matches, research.MatchRecord{
					MatchID:      matchID,
					SeasonID:     season.ID,
					Round:        round,
					ScheduledAt:  scheduled,
					HomeTeamUID:  home,
					AwayTeamUID:  away,
					HomeTeamName: fmt.Sprintf("Home %d", home),
					AwayTeamName: fmt.Sprintf("Away %d", away),
					HomeScore:    pointer(homeScore),
					AwayScore:    pointer(awayScore),
					Odds: []research.OddsQuote{
						{MarketID: research.OneXTwoMarketID, OutcomeID: "1", OutcomeName: "Home", Odds: 2, RawOdds: "2", ObservedAt: observation, SourceTimestampAt: observation, PointInTimeVerified: true, ObservationKind: "verified_test"},
						{MarketID: research.OneXTwoMarketID, OutcomeID: "X", OutcomeName: "Draw", Odds: 3, RawOdds: "3", ObservedAt: observation, SourceTimestampAt: observation, PointInTimeVerified: true, ObservationKind: "verified_test"},
						{MarketID: research.OneXTwoMarketID, OutcomeID: "2", OutcomeName: "Away", Odds: 4, RawOdds: "4", ObservedAt: observation, SourceTimestampAt: observation, PointInTimeVerified: true, ObservationKind: "verified_test"},
					},
				})
				matchID++
			}
		}
	}
	return research.NewDataset(matches, seasons)
}

func futureMutationCheck(dataset research.Dataset) {
	target := dataset.Matches[8]
	cutoff := target.ScheduledAt.Add(-30 * time.Second)
	before, err := research.BuildFeatureVector(dataset, target, cutoff, research.DefaultFeatureConfig())
	if err != nil {
		fail(err)
	}
	mutated := research.NewDataset(dataset.Matches, dataset.Seasons)
	for index := range mutated.Matches {
		if mutated.Matches[index].ScheduledAt.After(cutoff) {
			mutated.Matches[index].HomeScore = pointer(100)
			mutated.Matches[index].AwayScore = pointer(0)
		}
	}
	after, err := research.BuildFeatureVector(mutated, target, cutoff, research.DefaultFeatureConfig())
	if err != nil {
		fail(err)
	}
	if !before.EqualValues(after) {
		fail(fmt.Errorf("future mutation changed a pre-cutoff feature vector"))
	}
}

func pointer(value int) *int { return &value }

func fail(err error) {
	fmt.Println("Phase 2 offline verification failed:", err)
	panic(err)
}
