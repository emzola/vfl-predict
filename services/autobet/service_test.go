package autobet

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRankPredictionsSelectsHighestObservedOneXTwoProbability(t *testing.T) {
	items := []predictionRow{
		{
			MatchID:             1,
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.61, Draw: 0.20, Away: 0.19,
			}},
		},
		{
			MatchID:             2,
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.16, Draw: 0.20, Away: 0.64,
			}},
		},
		{
			MatchID:             3,
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.60, Draw: 0.20, Away: 0.20,
			}},
		},
		{
			MatchID:             4,
			PointInTimeVerified: false,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.90, Draw: 0.05, Away: 0.05,
			}},
		},
	}

	ranked := rankPredictions(items)
	if len(ranked) != 2 {
		t.Fatalf("ranked count=%d want 2 qualifying matches", len(ranked))
	}
	if ranked[0].MatchID != 2 {
		t.Fatalf("top match=%d want 2", ranked[0].MatchID)
	}
	if ranked[0].DecisionBasis != selectionRule || ranked[0].DecisionOutcome != "away" {
		t.Fatalf("top decision=%+v", ranked[0])
	}
	if math.Abs(ranked[0].MarketProbability-0.64) > 1e-9 || math.Abs(ranked[0].DecisionProbability-0.64) > 1e-9 {
		t.Fatalf("top market probability=%v want 0.64", ranked[0].MarketProbability)
	}
}

func TestRankPredictionsDoesNotUseModelProbability(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, Probability: probability{Home: 0.58, Draw: 0.22, Away: 0.20}},
		{MatchID: 2, Probability: probability{Home: 0.64, Draw: 0.19, Away: 0.17}},
	}

	ranked := rankPredictions(items)
	if len(ranked) != 0 {
		t.Fatalf("model-only predictions were incorrectly selected: %+v", ranked)
	}
}

func TestTopRankedPredictionKeepsOnlyRankOne(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, PointInTimeVerified: true, Bookmaker: &bookmakerPayload{Implied: probability{Home: 0.61, Draw: 0.20, Away: 0.19}}},
		{MatchID: 2, PointInTimeVerified: true, Bookmaker: &bookmakerPayload{Implied: probability{Home: 0.64, Draw: 0.19, Away: 0.17}}},
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
	if len(ranked) != 1 || ranked[0].DecisionBasis != selectionRule {
		t.Fatalf("decision basis=%s want %s", ranked[0].DecisionBasis, selectionRule)
	}
	if ranked[0].DecisionOutcome != "home" {
		t.Fatalf("decision outcome=%s want home", ranked[0].DecisionOutcome)
	}
	if math.Abs(ranked[0].MarketProbability-0.623) > 1e-9 {
		t.Fatalf("market probability=%v want 0.623", ranked[0].MarketProbability)
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
	want := []string{"even", "odd", "even", "odd", "odd", "even"}
	for index := 0; index < len(want)*2; index++ {
		got := oddEvenBetCycle[index%len(oddEvenBetCycle)].Name
		if got != want[index%len(want)] {
			t.Fatalf("cycle position %d=%q want %q", index, got, want[index%len(want)])
		}
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

func TestDetermineNextStakeUsesFlatBaseStake(t *testing.T) {
	if got := determineNextStake(50); math.Abs(got-50) > 1e-9 {
		t.Fatalf("determineNextStake=%v want 50", got)
	}
	if got := determineNextStake(50.567); math.Abs(got-50.57) > 1e-9 {
		t.Fatalf("determineNextStake rounded=%v want 50.57", got)
	}
	if got := determineNextStake(0); got != 0 {
		t.Fatalf("determineNextStake accepted zero base stake: %v", got)
	}
}
