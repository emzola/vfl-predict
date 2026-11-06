package autobet

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRankPredictionsSelectsThePredictionRankingLeader(t *testing.T) {
	items := []predictionRow{
		{
			MatchID:     1,
			Probability: probability{Home: 0.64, Draw: 0.20, Away: 0.16},
			Secondary:   &secondaryGoalPayload{Over25: 0.62},
		},
		{
			MatchID:             2,
			Probability:         probability{Home: 0.55, Draw: 0.25, Away: 0.20},
			Secondary:           &secondaryGoalPayload{Over25: 0.70},
			BookmakerGoalMarket: &bookmakerGoalMarketPayload{ModelDifference: 0.08, PointInTimeVerified: true},
		},
		{
			MatchID:             3,
			Probability:         probability{Home: 0.90, Draw: 0.05, Away: 0.05},
			Secondary:           &secondaryGoalPayload{Over25: 0.65},
			BookmakerGoalMarket: &bookmakerGoalMarketPayload{ModelDifference: 0.03, PointInTimeVerified: true},
		},
		{
			MatchID:     4,
			Probability: probability{Home: 0.80, Draw: 0.10, Away: 0.10},
		},
	}

	ranked := rankPredictions(items)
	if len(ranked) != len(items) {
		t.Fatalf("ranked count=%d want %d predictions", len(ranked), len(items))
	}
	if ranked[0].MatchID != 2 {
		t.Fatalf("top match=%d want 2", ranked[0].MatchID)
	}
	if ranked[0].DecisionBasis != "goal_market_difference" || ranked[0].DecisionOutcome != "over_2_5" {
		t.Fatalf("top decision=%+v", ranked[0])
	}
	if math.Abs(ranked[0].DecisionProbability-0.70) > 1e-9 {
		t.Fatalf("top decision probability=%v want 0.70", ranked[0].DecisionProbability)
	}
	if ranked[0].DecisionMarketDifference == nil || math.Abs(*ranked[0].DecisionMarketDifference-0.08) > 1e-9 {
		t.Fatalf("top market difference=%v want 0.08", ranked[0].DecisionMarketDifference)
	}
	for index, item := range ranked {
		if item.DecisionRank != index+1 {
			t.Fatalf("rank at index %d=%d want %d", index, item.DecisionRank, index+1)
		}
	}
}

func TestRankPredictionsUsesModelFallbackWithoutMarketGate(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, Probability: probability{Home: 0.58, Draw: 0.22, Away: 0.20}},
		{MatchID: 2, Probability: probability{Home: 0.64, Draw: 0.19, Away: 0.17}},
	}

	ranked := rankPredictions(items)
	if len(ranked) != 2 {
		t.Fatalf("ranked count=%d want 2 predictions", len(ranked))
	}
	if ranked[0].MatchID != 2 || ranked[0].DecisionBasis != "model_probability" || ranked[0].DecisionOutcome != "home" {
		t.Fatalf("top fallback decision=%+v", ranked[0])
	}
	if ranked[1].MatchID != 1 || ranked[1].DecisionRank != 2 {
		t.Fatalf("second fallback prediction=%+v", ranked[1])
	}
}

func TestTopRankedPredictionKeepsOnlyRankOne(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, Probability: probability{Home: 0.61, Draw: 0.20, Away: 0.19}},
		{MatchID: 2, Probability: probability{Home: 0.64, Draw: 0.19, Away: 0.17}},
	}

	top := topRankedPrediction(rankPredictions(items))
	if len(top) != 1 {
		t.Fatalf("top prediction count=%d want 1", len(top))
	}
	if top[0].DecisionRank != 1 || top[0].MatchID != 2 {
		t.Fatalf("top prediction=%+v want rank 1 match 2", top[0])
	}
}

func TestApplyPredictionPayloadRecomputesMarketDifferenceFromImplied(t *testing.T) {
	item := predictionRow{
		MatchID: 1,
		Probability: probability{
			Home: 0.663,
			Draw: 0.160,
			Away: 0.177,
		},
		PointInTimeVerified: true,
	}
	payload, err := json.Marshal(predictionPayload{
		Bookmaker: &bookmakerPayload{
			Implied: probability{
				Home: 0.623,
				Draw: 0.200,
				Away: 0.177,
			},
			// Simulate stale or missing stored deltas; backend should
			// recompute these from implied probabilities.
			ModelDifference: probability{},
		},
		BookmakerGoalMarket: &bookmakerGoalMarketPayload{
			Implied:             goalMarketProbability{Over25: 0.63, Under25: 0.37},
			PointInTimeVerified: true,
		},
		Secondary: &secondaryGoalPayload{Over25: 0.70, Under25: 0.30},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	applyPredictionPayload(&item, payload)

	if item.Bookmaker == nil {
		t.Fatal("bookmaker payload missing")
	}
	if got := item.Bookmaker.ModelDifference.Home; math.Abs(got-0.04) > 1e-9 {
		t.Fatalf("home model difference=%v want 0.04", got)
	}
	if got := item.Bookmaker.ModelDifference.Away; math.Abs(got-0) > 1e-9 {
		t.Fatalf("away model difference=%v want 0", got)
	}

	ranked := rankPredictions([]predictionRow{item})
	if len(ranked) != 1 || ranked[0].DecisionBasis != "goal_market_difference" {
		t.Fatalf("decision basis=%s want goal_market_difference", ranked[0].DecisionBasis)
	}
	if ranked[0].DecisionOutcome != "over_2_5" || math.Abs(ranked[0].DecisionProbability-0.70) > 1e-9 {
		t.Fatalf("decision=%+v want over_2_5 at 0.70", ranked[0])
	}
	if ranked[0].DecisionMarketDifference == nil || math.Abs(*ranked[0].DecisionMarketDifference-0.07) > 1e-9 {
		t.Fatalf("market difference=%v want 0.07", ranked[0].DecisionMarketDifference)
	}
}

func TestApplyPredictionPayloadLoadsSecondaryGoalMarkets(t *testing.T) {
	item := predictionRow{}
	payload, err := json.Marshal(predictionPayload{
		Secondary: &secondaryGoalPayload{
			Over25:           0.74,
			Under25:          0.26,
			BothTeamsToScore: 0.52,
			NeitherTeamScore: 0.48,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	applyPredictionPayload(&item, payload)

	if item.Secondary == nil || item.Secondary.Over25 != 0.74 || item.Secondary.BothTeamsToScore != 0.52 {
		t.Fatalf("secondary payload was not loaded: %+v", item.Secondary)
	}
}

func TestRoundMoneyRoundsToTwoDecimals(t *testing.T) {
	if got := roundMoney(1234.567); got != 1234.57 {
		t.Fatalf("roundMoney=%v want 1234.57", got)
	}
}

func TestOver15SettlementDecisionUsesFinalScore(t *testing.T) {
	status, score, settled := over15SettlementDecision("unknown", true, &matchScore{Home: 1, Away: 0}, nil, &matchScore{Home: 1, Away: 1})
	if !settled || status != "won" || score == nil || score.Home != 1 || score.Away != 1 {
		t.Fatalf("final Over 1.5 decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = over15SettlementDecision("unknown", true, &matchScore{Home: 0, Away: 0}, nil, &matchScore{Home: 1, Away: 0})
	if !settled || status != "lost" || score == nil || score.Home != 1 || score.Away != 0 {
		t.Fatalf("final Under 1.5 decision=(%s, %+v, %v)", status, score, settled)
	}
}

func TestOver15SettlementDecisionWaitsForFinalScore(t *testing.T) {
	status, score, settled := over15SettlementDecision("unknown", true, &matchScore{Home: 1, Away: 0}, &matchScore{Home: 1, Away: 0}, nil)
	if settled || status != "" || score != nil {
		t.Fatalf("unfinished Over 1.5 decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = over15SettlementDecision("completed", false, &matchScore{Home: 1, Away: 0}, nil, nil)
	if !settled || status != "lost" || score == nil {
		t.Fatalf("completed Under 1.5 decision=(%s, %+v, %v)", status, score, settled)
	}
}

func TestOddEvenBetCycleRepeats(t *testing.T) {
	want := []string{"odd", "even", "odd", "even", "odd", "odd", "even", "even"}
	for index := 0; index < len(want)*2; index++ {
		got := oddEvenBetCycle[index%len(oddEvenBetCycle)].Name
		if got != want[index%len(want)] {
			t.Fatalf("cycle position %d=%q want %q", index, got, want[index%len(want)])
		}
	}
}

func TestDoubleChance12SettlementDecisionUsesFinalScore(t *testing.T) {
	status, score, settled := doubleChance12SettlementDecision("unknown", true, &matchScore{Home: 1, Away: 0}, nil, &matchScore{Home: 2, Away: 1})
	if !settled || status != "won" || score == nil || score.Home != 2 || score.Away != 1 {
		t.Fatalf("final non-draw decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = doubleChance12SettlementDecision("unknown", true, &matchScore{Home: 1, Away: 0}, nil, &matchScore{Home: 1, Away: 1})
	if !settled || status != "lost" || score == nil || score.Home != 1 || score.Away != 1 {
		t.Fatalf("final draw decision=(%s, %+v, %v)", status, score, settled)
	}
}

func TestDoubleChance12SettlementDecisionWaitsForFinalScore(t *testing.T) {
	status, score, settled := doubleChance12SettlementDecision("unknown", true, &matchScore{Home: 1, Away: 0}, &matchScore{Home: 1, Away: 0}, nil)
	if settled || status != "" || score != nil {
		t.Fatalf("unfinished Double Chance 12 decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = doubleChance12SettlementDecision("completed", false, &matchScore{Home: 2, Away: 1}, nil, nil)
	if !settled || status != "won" || score == nil {
		t.Fatalf("completed non-draw decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = doubleChance12SettlementDecision("completed", false, &matchScore{Home: 1, Away: 1}, nil, nil)
	if !settled || status != "lost" || score == nil {
		t.Fatalf("completed draw decision=(%s, %+v, %v)", status, score, settled)
	}
}

func TestOddEvenSettlementDecisionUsesSelectedOutcome(t *testing.T) {
	status, score, settled := oddEvenSettlementDecisionFor("completed", false, &matchScore{Home: 1, Away: 1}, nil, nil, "even")
	if !settled || status != "won" || score == nil {
		t.Fatalf("even settlement=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = oddEvenSettlementDecisionFor("completed", false, &matchScore{Home: 1, Away: 1}, nil, nil, "odd")
	if !settled || status != "lost" || score == nil {
		t.Fatalf("odd settlement=(%s, %+v, %v)", status, score, settled)
	}
}

func TestApplySettledResultToRecoveryTracksLossesAndMissedProfits(t *testing.T) {
	recovery := applySettledResultToRecovery(recoveryState{}, "lost", 50, 1.65, -50)
	if recovery.UnrecoveredLoss != 50 || recovery.MissedProfit != 32.50 || recovery.obligation() != 82.50 {
		t.Fatalf("recovery after loss=%+v obligation=%v", recovery, recovery.obligation())
	}

	recovery = applySettledResultToRecovery(recovery, "lost", 176.93, 1.65, -176.93)
	if recovery.UnrecoveredLoss != 226.93 || recovery.MissedProfit != 147.50 || recovery.obligation() != 374.43 {
		t.Fatalf("recovery after recovery loss=%+v obligation=%v", recovery, recovery.obligation())
	}
}

func TestApplySettledResultToRecoveryUsesWinningProfitToReduceOutstandingBalances(t *testing.T) {
	recovery := applySettledResultToRecovery(recoveryState{}, "lost", 50, 1.65, -50)
	recovery = applySettledResultToRecovery(recovery, "won", 126.93, 1.65, 32.50)
	if recovery.UnrecoveredLoss != 17.50 || recovery.MissedProfit != 32.50 || recovery.obligation() != 50 {
		t.Fatalf("recovery after partial win=%+v obligation=%v", recovery, recovery.obligation())
	}

	recovery = applySettledResultToRecovery(recovery, "won", 176.93, 1.65, 82.50)
	if recovery.obligation() != 0 {
		t.Fatalf("recovery after full recovery win=%+v", recovery)
	}
}

func TestDetermineMartingaleStakeCoversLossesMissedProfitAndCurrentProfit(t *testing.T) {
	got, err := determineMartingaleStake(50, 50, 32.50, 1.65)
	if err != nil {
		t.Fatalf("determineMartingaleStake error: %v", err)
	}
	if math.Abs(got-176.93) > 1e-9 {
		t.Fatalf("determineMartingaleStake=%v want 176.93", got)
	}
	if realizedProfit := roundMoney(got*1.65) - got; realizedProfit < 115.00 {
		t.Fatalf("recovery profit=%v want at least 115.00", realizedProfit)
	}
}

func TestDetermineMartingaleStakeStartsAtBaseAndRejectsInvalidInputs(t *testing.T) {
	got, err := determineMartingaleStake(50, 0, 0, 1.65)
	if err != nil {
		t.Fatalf("determineMartingaleStake fresh cycle error: %v", err)
	}
	if math.Abs(got-50) > 1e-9 {
		t.Fatalf("determineMartingaleStake fresh cycle=%v want 50", got)
	}

	invalid := [][4]float64{
		{0, 0, 0, 1.65},
		{50, -1, 0, 1.65},
		{50, 0, -1, 1.65},
		{50, 0, 0, 1.00},
		{50, math.NaN(), 0, 1.65},
	}
	for _, values := range invalid {
		if _, err := determineMartingaleStake(values[0], values[1], values[2], values[3]); err == nil {
			t.Fatalf("determineMartingaleStake accepted invalid inputs %+v", values)
		}
	}
}
