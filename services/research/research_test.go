package research

import (
	"math"
	"reflect"
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func syntheticDataset() Dataset {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	seasons := []SeasonRecord{
		{ID: "2024", StartUTC: base, EndUTC: base.AddDate(0, 0, 29)},
		{ID: "2025", StartUTC: base.AddDate(1, 0, 0), EndUTC: base.AddDate(1, 0, 29)},
		{ID: "2026", StartUTC: base.AddDate(2, 0, 0), EndUTC: base.AddDate(2, 0, 29)},
	}
	matches := make([]MatchRecord, 0, 3*4*8)
	var matchID provider.MatchID = 1000
	for seasonIndex, season := range seasons {
		for round := 1; round <= 4; round++ {
			for pairing := 0; pairing < 8; pairing++ {
				homeUID := provider.TeamUID(pairing + 1)
				awayUID := provider.TeamUID(pairing + 9)
				scheduled := season.StartUTC.Add(time.Duration(round-1) * 24 * time.Hour)
				homeScore := (seasonIndex + round + pairing) % 4
				awayScore := (seasonIndex + pairing) % 3
				match := MatchRecord{
					MatchID:      matchID,
					SeasonID:     season.ID,
					Round:        round,
					ScheduledAt:  scheduled,
					HomeTeamUID:  homeUID,
					AwayTeamUID:  awayUID,
					HomeTeamName: "Home " + homeUID.String(),
					AwayTeamName: "Away " + awayUID.String(),
					HomeScore:    intPointer(homeScore),
					AwayScore:    intPointer(awayScore),
				}
				observation := scheduled.Add(-2 * time.Hour)
				match.Odds = []OddsQuote{
					{MarketID: OneXTwoMarketID, OutcomeID: "1", OutcomeName: "Home", Odds: 2.0, RawOdds: "2.0", ObservedAt: observation, SourceTimestampAt: observation, PointInTimeVerified: true, ObservationKind: "verified_test"},
					{MarketID: OneXTwoMarketID, OutcomeID: "X", OutcomeName: "Draw", Odds: 3.5, RawOdds: "3.5", ObservedAt: observation, SourceTimestampAt: observation, PointInTimeVerified: true, ObservationKind: "verified_test"},
					{MarketID: OneXTwoMarketID, OutcomeID: "2", OutcomeName: "Away", Odds: 4.0, RawOdds: "4.0", ObservedAt: observation, SourceTimestampAt: observation, PointInTimeVerified: true, ObservationKind: "verified_test"},
				}
				matches = append(matches, match)
				matchID++
			}
		}
	}
	return NewDataset(matches, seasons)
}

func intPointer(value int) *int { return &value }

func TestFeaturePipelineExcludesFutureResults(t *testing.T) {
	dataset := syntheticDataset()
	var target MatchRecord
	for _, match := range dataset.Matches {
		if match.SeasonID == "2026" && match.Round == 2 && match.HomeTeamUID == 1 {
			target = match
			break
		}
	}
	cutoff := target.ScheduledAt.Add(-30 * time.Second)
	before, err := BuildFeatureVector(dataset, target, cutoff, DefaultFeatureConfig())
	if err != nil {
		t.Fatal(err)
	}
	mutated := NewDataset(dataset.Matches, dataset.Seasons)
	for index := range mutated.Matches {
		if mutated.Matches[index].SeasonID == "2026" && mutated.Matches[index].Round >= 3 {
			mutated.Matches[index].HomeScore = intPointer(99)
			mutated.Matches[index].AwayScore = intPointer(0)
		}
	}
	after, err := BuildFeatureVector(mutated, target, cutoff, DefaultFeatureConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !before.EqualValues(after) {
		t.Fatalf("future result mutation changed point-in-time features: before=%v after=%v", before.Values, after.Values)
	}

	// A same-round result is also future information when the round cutoff is
	// before the first kickoff.
	for index := range mutated.Matches {
		if mutated.Matches[index].SeasonID == target.SeasonID && mutated.Matches[index].Round == target.Round && mutated.Matches[index].MatchID != target.MatchID {
			mutated.Matches[index].HomeScore = intPointer(88)
			mutated.Matches[index].AwayScore = intPointer(0)
		}
	}
	sameRound, err := BuildFeatureVector(mutated, target, cutoff, DefaultFeatureConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !before.EqualValues(sameRound) {
		t.Fatal("same-round result mutation leaked into features")
	}
}

func TestFeatureSeasonBoundaryAndStableIdentifiers(t *testing.T) {
	dataset := syntheticDataset()
	var first MatchRecord
	for _, match := range dataset.Matches {
		if match.SeasonID == "2026" && match.Round == 1 {
			first = match
			break
		}
	}
	vector, err := BuildFeatureVector(dataset, first, first.ScheduledAt.Add(-time.Minute), DefaultFeatureConfig())
	if err != nil {
		t.Fatal(err)
	}
	if vector.Values["previous_season_available"] != 1 {
		t.Fatalf("previous season was not available: %#v", vector.Values)
	}
	if vector.Values["home_current_position"] != 0 {
		t.Fatalf("first-round current position should be unavailable, got %v", vector.Values["home_current_position"])
	}
	if vector.MatchID != first.MatchID || vector.SeasonID != first.SeasonID {
		t.Fatal("feature vector identifiers are not canonical")
	}
}

func TestOddsOverroundAndInvalidOrUnverifiedHandling(t *testing.T) {
	dataset := syntheticDataset()
	line, ok := dataset.Matches[0].OddsLine(false)
	if !ok {
		t.Fatal("verified 1X2 line was not detected")
	}
	if err := line.Validate(); err != nil {
		t.Fatal(err)
	}
	probabilities := line.ImpliedProbabilities()
	if err := probabilities.Validate(1e-9); err != nil {
		t.Fatal(err)
	}
	unverified := dataset.Matches[0]
	for index := range unverified.Odds {
		unverified.Odds[index].PointInTimeVerified = false
		unverified.Odds[index].ObservationKind = "historical_returned_after_settlement"
	}
	if _, ok := unverified.OddsLine(false); ok {
		t.Fatal("unverified odds were accepted by the strict selector")
	}
	if _, ok := unverified.OddsLine(true); !ok {
		t.Fatal("explicit retrospective odds opt-in was not honored")
	}
}

func TestGoalTotalsOddsSelectVerifiedPointInTimeOver25Line(t *testing.T) {
	cutoff := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	match := MatchRecord{
		MatchID:      provider.MatchID(9002),
		HomeTeamUID:  provider.TeamUID(1),
		AwayTeamUID:  provider.TeamUID(2),
		HomeTeamName: "Home",
		AwayTeamName: "Away",
		Odds: []OddsQuote{
			{MarketID: GoalTotalsMarketID, Specifiers: GoalTotalsSpecifier25, OutcomeID: GoalTotalsOverOutcome, OutcomeName: "over 2.5", Odds: 2.0, RawOdds: "2.0", ObservedAt: cutoff.Add(-time.Minute), SourceTimestampAt: cutoff.Add(-time.Minute), PointInTimeVerified: true},
			{MarketID: GoalTotalsMarketID, Specifiers: GoalTotalsSpecifier25, OutcomeID: GoalTotalsUnderOutcome, OutcomeName: "under 2.5", Odds: 1.8, RawOdds: "1.8", ObservedAt: cutoff.Add(-time.Minute), SourceTimestampAt: cutoff.Add(-time.Minute), PointInTimeVerified: true},
			{MarketID: GoalTotalsMarketID, Specifiers: GoalTotalsSpecifier25, OutcomeID: GoalTotalsOverOutcome, OutcomeName: "over 2.5", Odds: 9.0, RawOdds: "9.0", ObservedAt: cutoff.Add(time.Minute), SourceTimestampAt: cutoff.Add(time.Minute), PointInTimeVerified: true},
			{MarketID: GoalTotalsMarketID, Specifiers: GoalTotalsSpecifier25, OutcomeID: GoalTotalsUnderOutcome, OutcomeName: "under 2.5", Odds: 9.0, RawOdds: "9.0", ObservedAt: cutoff.Add(time.Minute), SourceTimestampAt: cutoff.Add(time.Minute), PointInTimeVerified: true},
		},
	}

	line, ok := FindOverUnder25OddsAt(match, cutoff)
	if !ok {
		t.Fatal("verified point-in-time Over/Under 2.5 line was not detected")
	}
	if line.Over25 != 2.0 || line.Under25 != 1.8 {
		t.Fatalf("future goal-market quote leaked into cutoff line: line=%+v", line)
	}
	if err := line.Validate(); err != nil {
		t.Fatal(err)
	}
	implied := line.ImpliedProbabilities()
	if math.Abs(implied.Over25+implied.Under25-1) > 1e-9 {
		t.Fatalf("goal-market implied probabilities do not normalize: %+v", implied)
	}
}

func TestBacktestOddsRespectCutoffUnlessRetrospectiveOptIn(t *testing.T) {
	cutoff := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	match := MatchRecord{
		MatchID: provider.MatchID(9001), HomeTeamUID: provider.TeamUID(1), AwayTeamUID: provider.TeamUID(2),
		HomeTeamName: "Home", AwayTeamName: "Away",
		Odds: []OddsQuote{
			{MarketID: OneXTwoMarketID, OutcomeID: "1", OutcomeName: "Home", Odds: 2, RawOdds: "2", ObservedAt: cutoff.Add(-time.Minute), SourceTimestampAt: cutoff.Add(-time.Minute), PointInTimeVerified: true},
			{MarketID: OneXTwoMarketID, OutcomeID: "3", OutcomeName: "Draw", Odds: 3, RawOdds: "3", ObservedAt: cutoff.Add(-time.Minute), SourceTimestampAt: cutoff.Add(-time.Minute), PointInTimeVerified: true},
			{MarketID: OneXTwoMarketID, OutcomeID: "2", OutcomeName: "Away", Odds: 4, RawOdds: "4", ObservedAt: cutoff.Add(-time.Minute), SourceTimestampAt: cutoff.Add(-time.Minute), PointInTimeVerified: true},
			{MarketID: OneXTwoMarketID, OutcomeID: "1", OutcomeName: "Home", Odds: 99, RawOdds: "99", ObservedAt: cutoff.Add(time.Minute), SourceTimestampAt: cutoff.Add(time.Minute), PointInTimeVerified: true},
			{MarketID: OneXTwoMarketID, OutcomeID: "3", OutcomeName: "Draw", Odds: 99, RawOdds: "99", ObservedAt: cutoff.Add(time.Minute), SourceTimestampAt: cutoff.Add(time.Minute), PointInTimeVerified: true},
			{MarketID: OneXTwoMarketID, OutcomeID: "2", OutcomeName: "Away", Odds: 99, RawOdds: "99", ObservedAt: cutoff.Add(time.Minute), SourceTimestampAt: cutoff.Add(time.Minute), PointInTimeVerified: true},
		},
	}
	asOf := match
	asOf.Odds = oddsAtBacktestCutoff(match.Odds, cutoff, false)
	line, ok := backtestOddsLine(asOf, cutoff, false)
	if !ok || line.Home != 2 || line.Draw != 3 || line.Away != 4 {
		t.Fatalf("future verified quote leaked into cutoff line: line=%+v ok=%t", line, ok)
	}
	retrospective := match
	retrospective.Odds = oddsAtBacktestCutoff(match.Odds, cutoff, true)
	line, ok = backtestOddsLine(retrospective, cutoff, true)
	if !ok || line.Home != 99 {
		t.Fatalf("retrospective opt-in did not use latest returned line: line=%+v ok=%t", line, ok)
	}
}

func TestBaselineModelsAndPoissonDerivedProbabilities(t *testing.T) {
	dataset := syntheticDataset()
	for _, name := range BaselineModelNames() {
		model, err := NewBaselineModel(name, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := model.Fit(dataset.Matches[:16]); err != nil {
			t.Fatal(err)
		}
		probability := model.Predict(dataset.Matches[20])
		if err := probability.Validate(1e-8); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	poisson := NewPoissonGoalModel()
	if err := poisson.Fit(dataset.Matches[:16]); err != nil {
		t.Fatal(err)
	}
	goals := poisson.PredictGoals(dataset.Matches[20], 8)
	if goals.LambdaHome <= 0 || goals.LambdaAway <= 0 || goals.ResidualTail < 0 || goals.ResidualTail >= 1 {
		t.Fatalf("unexpected Poisson output: %#v", goals)
	}
	if len(goals.CorrectScores) != 81 {
		t.Fatalf("correct-score grid size=%d want 81", len(goals.CorrectScores))
	}
}

func TestBookmakerFirstUsesVerifiedMarketAndPoissonFallback(t *testing.T) {
	dataset := syntheticDataset()
	model := NewBookmakerFirstModel(false)
	if err := model.Fit(dataset.Matches[:16]); err != nil {
		t.Fatal(err)
	}

	marketMatch := dataset.Matches[20]
	marketLine, ok := marketMatch.OddsLine(false)
	if !ok {
		t.Fatal("synthetic match has no verified market line")
	}
	marketProbability := model.Predict(marketMatch)
	wantMarket := marketLine.ImpliedProbabilities()
	if marketProbability != wantMarket {
		t.Fatalf("bookmaker-first probability=%+v want=%+v", marketProbability, wantMarket)
	}
	if !model.UsesBookmaker(marketMatch) {
		t.Fatal("bookmaker-first did not report market usage")
	}

	fallbackMatch := marketMatch
	fallbackMatch.Odds = append([]OddsQuote(nil), marketMatch.Odds...)
	for index := range fallbackMatch.Odds {
		fallbackMatch.Odds[index].PointInTimeVerified = false
	}
	fallbackProbability := model.Predict(fallbackMatch)
	wantFallback := model.Statistical.Predict(fallbackMatch)
	if fallbackProbability != wantFallback {
		t.Fatalf("bookmaker-first fallback=%+v want Poisson=%+v", fallbackProbability, wantFallback)
	}
	if model.UsesBookmaker(fallbackMatch) {
		t.Fatal("unverified market was reported as used")
	}
}

func TestWalkForwardBacktestIsDeterministicAndChronological(t *testing.T) {
	dataset := syntheticDataset()
	config := DefaultBacktestConfig()
	config.Models = []string{"overall_frequency", "poisson", "bookmaker_implied"}
	config.BootstrapReplicates = 25
	config.AllowRetrospectiveOdds = false
	first, err := RunBacktest(dataset, config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunBacktest(dataset, config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical backtest inputs produced different results")
	}
	if len(first.SeasonsEvaluated) != 2 || first.SeasonsEvaluated[0] != "2025" {
		t.Fatalf("unexpected chronological evaluation seasons: %#v", first.SeasonsEvaluated)
	}
	for _, evaluation := range first.Evaluations {
		if evaluation.Metrics.Count == 0 {
			t.Fatalf("model %s has no evaluation rows", evaluation.ModelName)
		}
		if evaluation.Metrics.LogLoss < 0 || evaluation.Metrics.Brier < 0 {
			t.Fatalf("invalid scoring metrics: %#v", evaluation.Metrics)
		}
	}
}

func TestBettingROIArithmeticAndRetrospectiveGuard(t *testing.T) {
	dataset := syntheticDataset()
	actual, _ := dataset.Matches[0].Outcome()
	line, _ := dataset.Matches[0].OddsLine(false)
	prediction := BacktestPrediction{
		ModelName:      "test",
		SeasonID:       dataset.Matches[0].SeasonID,
		MatchID:        dataset.Matches[0].MatchID,
		Round:          1,
		PredictionAsOf: dataset.Matches[0].ScheduledAt.Add(-time.Hour),
		Probability:    Probability{Home: 0.8, Draw: 0.1, Away: 0.1},
		Actual:         actual,
		Odds:           &line,
		OddsTrack:      "verified_point_in_time",
	}
	report := AnalyzeBetting([]BacktestPrediction{prediction}, DefaultBettingConfig())
	if report.Aggregate.Selections != 1 || report.Aggregate.TotalStake != 1 {
		t.Fatalf("unexpected betting summary: %#v", report.Aggregate)
	}
	if report.Aggregate.TotalReturn != line.Home || report.Aggregate.ROI != line.Home-1 {
		t.Fatalf("ROI arithmetic mismatch: %#v", report.Aggregate)
	}
	unverified := prediction
	unverified.Odds = &OddsLine{Home: 2, Draw: 3, Away: 4, HomeQuote: OddsQuote{Odds: 2}, DrawQuote: OddsQuote{Odds: 3}, AwayQuote: OddsQuote{Odds: 4}}
	guarded := AnalyzeBetting([]BacktestPrediction{unverified}, DefaultBettingConfig())
	if guarded.Aggregate.Selections != 0 || guarded.SkippedUnverified != 1 {
		t.Fatalf("unverified odds were not guarded: %#v", guarded)
	}
}
