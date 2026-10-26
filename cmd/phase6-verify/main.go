package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"bet9ja-vfl/services/research"
)

type check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func main() {
	checks := []check{
		checkScorelineMarkets(),
		checkDynamicState(),
		checkReplayLeakage(),
	}
	passed := true
	for _, value := range checks {
		fmt.Printf("%-28s %v %s\n", value.Name, value.Passed, value.Detail)
		passed = passed && value.Passed
	}
	encoded, _ := json.MarshalIndent(map[string]any{"phase": 6, "network_requests": 0, "checks": checks}, "", "  ")
	fmt.Println(string(encoded))
	if !passed {
		os.Exit(1)
	}
}

func checkScorelineMarkets() check {
	distribution, err := research.NewScorelineDistribution(1.45, 0.95, 0)
	if err != nil {
		return check{"scoreline_matrix", false, err.Error()}
	}
	prediction := research.GoalPredictionFromScoreline(distribution)
	if err := distribution.Validate(1e-10); err != nil {
		return check{"scoreline_matrix", false, err.Error()}
	}
	if err := prediction.Probability.Validate(1e-10); err != nil || math.Abs(prediction.Over25+prediction.Under25-1) > 1e-10 || math.Abs(prediction.BothTeamsToScore+prediction.NeitherTeamScores-1) > 1e-10 {
		return check{"scoreline_matrix", false, "market probabilities are inconsistent"}
	}
	return check{"scoreline_matrix", true, fmt.Sprintf("adaptive grid=%dx%d tail=%.3g", distribution.MaxGoals+1, distribution.MaxGoals+1, distribution.TailMass)}
}

func checkDynamicState() check {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	matches := []research.MatchRecord{
		{MatchID: 1, SeasonID: "2026", Round: 1, ScheduledAt: base, HomeTeamUID: 1, AwayTeamUID: 2, HomeScore: intPointer(3), AwayScore: intPointer(0)},
		{MatchID: 2, SeasonID: "2026", Round: 2, ScheduledAt: base.Add(24 * time.Hour), HomeTeamUID: 2, AwayTeamUID: 3, HomeScore: intPointer(1), AwayScore: intPointer(1)},
	}
	model := research.NewDynamicBayesianModel()
	if err := model.Fit(matches); err != nil {
		return check{"dynamic_state", false, err.Error()}
	}
	model.PrepareSeason("2026")
	prediction := model.PredictGoals(research.MatchRecord{HomeTeamUID: 1, AwayTeamUID: 3}, 0)
	if err := prediction.Probability.Validate(1e-10); err != nil {
		return check{"dynamic_state", false, err.Error()}
	}
	return check{"dynamic_state", true, fmt.Sprintf("matches=%d home_lambda=%.3f away_lambda=%.3f", model.TotalMatches, prediction.LambdaHome, prediction.LambdaAway)}
}

func checkReplayLeakage() check {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	matches := []research.MatchRecord{
		{MatchID: 1, SeasonID: "2024", Round: 1, ScheduledAt: base, HomeTeamUID: 1, AwayTeamUID: 2, HomeScore: intPointer(1), AwayScore: intPointer(0)},
		{MatchID: 2, SeasonID: "2025", Round: 1, ScheduledAt: base.AddDate(1, 0, 0), HomeTeamUID: 1, AwayTeamUID: 2, HomeScore: intPointer(0), AwayScore: intPointer(1)},
		{MatchID: 3, SeasonID: "2025", Round: 2, ScheduledAt: base.AddDate(1, 0, 0).Add(24 * time.Hour), HomeTeamUID: 1, AwayTeamUID: 2, HomeScore: intPointer(4), AwayScore: intPointer(0)},
	}
	config := research.DefaultDynamicReplayConfig()
	config.BootstrapReplicates = 0
	first, err := research.RunDynamicBayesianReplay(research.NewDataset(matches, nil), config)
	if err != nil || len(first.Predictions) != 2 {
		return check{"chronological_replay", false, fmt.Sprintf("err=%v predictions=%d", err, len(first.Predictions))}
	}
	mutated := append([]research.MatchRecord(nil), matches...)
	mutated[2].HomeScore = intPointer(0)
	mutated[2].AwayScore = intPointer(7)
	second, err := research.RunDynamicBayesianReplay(research.NewDataset(mutated, nil), config)
	if err != nil || len(second.Predictions) != 2 || first.Predictions[0].Probability != second.Predictions[0].Probability {
		return check{"chronological_replay", false, "future result affected an earlier prediction"}
	}
	return check{"chronological_replay", true, "future results do not alter earlier cutoff predictions"}
}

func intPointer(value int) *int { return &value }
