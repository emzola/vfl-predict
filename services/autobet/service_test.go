package autobet

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRankPredictionsKeepsObservedMarketProbabilitiesAbove50(t *testing.T) {
	items := []predictionRow{
		{
			MatchID:             1,
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.51, Draw: 0.25, Away: 0.24,
			}},
		},
		{
			MatchID:             2,
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.16, Draw: 0.26, Away: 0.58,
			}},
		},
		{
			MatchID:             3,
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.50, Draw: 0.25, Away: 0.25,
			}},
		},
		{
			MatchID:             4,
			PointInTimeVerified: false,
			Bookmaker: &bookmakerPayload{Implied: probability{
				Home: 0.70, Draw: 0.20, Away: 0.10,
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
	if math.Abs(ranked[0].MarketProbability-0.58) > 1e-9 || math.Abs(ranked[0].DecisionProbability-0.58) > 1e-9 {
		t.Fatalf("top market probability=%v want 0.58", ranked[0].MarketProbability)
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

func TestRankPredictionsKeepsEveryQualifyingMatch(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, PointInTimeVerified: true, Bookmaker: &bookmakerPayload{Implied: probability{Home: 0.60, Draw: 0.20, Away: 0.20}}},
		{MatchID: 2, PointInTimeVerified: true, Bookmaker: &bookmakerPayload{Implied: probability{Home: 0.20, Draw: 0.20, Away: 0.60}}},
	}

	ranked := rankPredictions(items)
	if len(ranked) != 2 {
		t.Fatalf("qualifying prediction count=%d want 2", len(ranked))
	}
	if ranked[0].DecisionRank != 1 || ranked[1].DecisionRank != 2 {
		t.Fatalf("qualifying prediction ranks=%+v", ranked)
	}
}

func TestApplyPredictionPayloadRecomputesMarketDifferenceFromImplied(t *testing.T) {
	item := predictionRow{
		MatchID: 1,
		Probability: probability{
			Home: 0.563,
			Draw: 0.246,
			Away: 0.292,
		},
		PointInTimeVerified: true,
	}
	payload, err := json.Marshal(predictionPayload{
		Bookmaker: &bookmakerPayload{
			Implied: probability{
				Home: 0.523,
				Draw: 0.300,
				Away: 0.277,
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
	if got := item.Bookmaker.ModelDifference.Away; math.Abs(got-0.015) > 1e-9 {
		t.Fatalf("away model difference=%v want 0.015", got)
	}

	ranked := rankPredictions([]predictionRow{item})
	if len(ranked) != 1 || ranked[0].DecisionBasis != selectionRule {
		t.Fatalf("decision basis=%s want %s", ranked[0].DecisionBasis, selectionRule)
	}
	if ranked[0].DecisionOutcome != "home" {
		t.Fatalf("decision outcome=%s want home", ranked[0].DecisionOutcome)
	}
	if math.Abs(ranked[0].MarketProbability-0.523) > 1e-9 {
		t.Fatalf("market probability=%v want 0.523", ranked[0].MarketProbability)
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

func TestOneXTwoSettlementDecisionUsesRequestedOutcome(t *testing.T) {
	status, score, settled := oneXTwoSettlementDecision("unknown", true, &matchScore{Home: 2, Away: 0}, nil, &matchScore{Home: 2, Away: 0}, "home")
	if !settled || status != "won" || score == nil || score.Home != 2 || score.Away != 0 {
		t.Fatalf("final home decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = oneXTwoSettlementDecision("completed", false, &matchScore{Home: 0, Away: 2}, nil, nil, "home")
	if !settled || status != "lost" || score == nil {
		t.Fatalf("home decision on away win=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = oneXTwoSettlementDecision("completed", false, &matchScore{Home: 0, Away: 2}, nil, nil, "away")
	if !settled || status != "won" || score == nil {
		t.Fatalf("away decision=(%s, %+v, %v)", status, score, settled)
	}

	status, score, settled = oneXTwoSettlementDecision("completed", false, &matchScore{Home: 1, Away: 1}, nil, nil, "away")
	if !settled || status != "lost" || score == nil {
		t.Fatalf("draw decision=(%s, %+v, %v)", status, score, settled)
	}
}

func TestOneXTwoSettlementDecisionWaitsForFinalScore(t *testing.T) {
	status, score, settled := oneXTwoSettlementDecision("unknown", true, &matchScore{Home: 1, Away: 0}, &matchScore{Home: 1, Away: 0}, nil, "home")
	if settled || status != "" || score != nil {
		t.Fatalf("unfinished 1X2 decision=(%s, %+v, %v)", status, score, settled)
	}
}

func TestDetermineFlatStakeAlwaysUsesBaseStake(t *testing.T) {
	got, err := determineFlatStake(50.126)
	if err != nil {
		t.Fatalf("determineFlatStake error: %v", err)
	}
	if math.Abs(got-50.13) > 1e-9 {
		t.Fatalf("determineFlatStake=%v want 50.13", got)
	}

	if _, err := determineFlatStake(0); err == nil {
		t.Fatal("determineFlatStake accepted zero base stake")
	}
	if _, err := determineFlatStake(math.NaN()); err == nil {
		t.Fatal("determineFlatStake accepted NaN base stake")
	}
}
