package research

import (
	"math"
	"reflect"
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func dynamicMatch(id provider.MatchID, season provider.SeasonID, round int, at time.Time, home, away provider.TeamUID, homeGoals, awayGoals int) MatchRecord {
	return MatchRecord{
		MatchID:     id,
		SeasonID:    season,
		Round:       round,
		ScheduledAt: at,
		HomeTeamUID: home,
		AwayTeamUID: away,
		HomeScore:   intPointer(homeGoals),
		AwayScore:   intPointer(awayGoals),
	}
}

func TestScorelineDistributionDerivesConsistentMarkets(t *testing.T) {
	distribution, err := NewScorelineDistribution(1.35, 0.85, 12)
	if err != nil {
		t.Fatal(err)
	}
	if err := distribution.Validate(1e-10); err != nil {
		t.Fatal(err)
	}
	prediction := GoalPredictionFromScoreline(distribution)
	if err := prediction.Probability.Validate(1e-10); err != nil {
		t.Fatal(err)
	}
	if math.Abs(prediction.Over25+prediction.Under25-1) > 1e-10 {
		t.Fatalf("over/under does not complement: %.12f %.12f", prediction.Over25, prediction.Under25)
	}
	if math.Abs(prediction.BothTeamsToScore+prediction.NeitherTeamScores-1) > 1e-10 {
		t.Fatalf("BTTS does not complement: %.12f %.12f", prediction.BothTeamsToScore, prediction.NeitherTeamScores)
	}
	if len(prediction.CorrectScores) != 169 {
		t.Fatalf("correct-score grid size=%d want 169", len(prediction.CorrectScores))
	}
	correctSum := 0.0
	for _, value := range prediction.CorrectScores {
		correctSum += value
	}
	if math.Abs(correctSum-1) > 1e-10 {
		t.Fatalf("correct scores sum to %.12f", correctSum)
	}
}

func TestDynamicBayesianModelIsDeterministicAndOpponentAdjusted(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	matches := []MatchRecord{
		dynamicMatch(1, "2026", 1, base, 1, 2, 4, 0),
		dynamicMatch(2, "2026", 1, base.Add(time.Hour), 1, 3, 3, 0),
		dynamicMatch(3, "2026", 2, base.Add(24*time.Hour), 2, 3, 1, 1),
		dynamicMatch(4, "2026", 2, base.Add(25*time.Hour), 3, 1, 0, 2),
	}
	target := MatchRecord{MatchID: 10, SeasonID: "2026", Round: 3, ScheduledAt: base.Add(48 * time.Hour), HomeTeamUID: 1, AwayTeamUID: 3}
	first := NewDynamicBayesianModel()
	second := NewDynamicBayesianModel()
	if err := first.Fit(matches); err != nil {
		t.Fatal(err)
	}
	if err := second.Fit(matches); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.StateSnapshot(), second.StateSnapshot()) {
		t.Fatal("same chronological input produced different dynamic states")
	}
	firstPrediction := first.PredictGoals(target, 12)
	secondPrediction := second.PredictGoals(target, 12)
	if !reflect.DeepEqual(firstPrediction, secondPrediction) {
		t.Fatal("same chronological input produced different predictions")
	}
	if err := firstPrediction.Probability.Validate(1e-10); err != nil {
		t.Fatal(err)
	}
	if firstPrediction.LambdaHome <= firstPrediction.LambdaAway {
		t.Fatalf("strong home evidence did not produce higher home expected goals: %#v", firstPrediction)
	}
	if first.Diagnostics(target).TotalMatchesUsed != len(matches) {
		t.Fatal("dynamic diagnostics did not report all completed matches")
	}
}

func TestDynamicBayesianModelHandlesSeasonResetAndMissingResults(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	matches := []MatchRecord{
		dynamicMatch(1, "2024", 1, base, 1, 2, 3, 0),
		{MatchID: 2, SeasonID: "2024", Round: 2, ScheduledAt: base.Add(24 * time.Hour), HomeTeamUID: 1, AwayTeamUID: 2},
		dynamicMatch(3, "2025", 1, base.AddDate(1, 0, 0), 2, 1, 0, 1),
	}
	model := NewDynamicBayesianModel()
	if err := model.Fit(matches); err != nil {
		t.Fatal(err)
	}
	if model.CurrentSeason != "2025" || model.TotalMatches != 2 || model.SeasonMatches != 1 {
		t.Fatalf("unexpected reset state: season=%s total=%d current=%d", model.CurrentSeason, model.TotalMatches, model.SeasonMatches)
	}
	for _, state := range model.StateSnapshot() {
		if state.RatingVariance <= 0 || state.AttackVariance <= 0 || state.DefenseVariance <= 0 {
			t.Fatalf("invalid uncertainty after season reset: %#v", state)
		}
	}
	empty := NewDynamicBayesianModel()
	if err := empty.Fit(nil); err != nil {
		t.Fatal(err)
	}
	prediction := empty.PredictGoals(MatchRecord{HomeTeamUID: 1, AwayTeamUID: 2}, 12)
	if err := prediction.Probability.Validate(1e-10); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicBayesianFactoryAndVersion(t *testing.T) {
	model, err := NewBaselineModel(DynamicBayesianModelName, false)
	if err != nil {
		t.Fatal(err)
	}
	if model.Name() != DynamicBayesianModelName || model.Version() != DynamicBayesianModelVersion {
		t.Fatalf("unexpected model identity: %s %s", model.Name(), model.Version())
	}
}
