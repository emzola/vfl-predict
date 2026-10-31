package autobet

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRankPredictionsPrefersPositiveOver25MarketDifference(t *testing.T) {
	items := []predictionRow{
		{
			MatchID: 1,
			Probability: probability{
				Home: 0.60, Draw: 0.20, Away: 0.20,
			},
			Secondary: &secondaryGoalPayload{Over25: 0.70},
			BookmakerGoalMarket: &bookmakerGoalMarketPayload{
				Implied:             goalMarketProbability{Over25: 0.60},
				PointInTimeVerified: true,
			},
		},
		{
			MatchID: 2,
			Probability: probability{
				Home: 0.30, Draw: 0.20, Away: 0.50,
			},
			Secondary: &secondaryGoalPayload{Over25: 0.75},
			BookmakerGoalMarket: &bookmakerGoalMarketPayload{
				Implied:             goalMarketProbability{Over25: 0.55},
				PointInTimeVerified: true,
			},
		},
	}

	ranked := rankPredictions(items)
	if len(ranked) != 2 {
		t.Fatalf("ranked count=%d want 2", len(ranked))
	}
	if ranked[0].MatchID != 2 {
		t.Fatalf("top match=%d want 2", ranked[0].MatchID)
	}
	if ranked[0].DecisionBasis != "goal_market_difference" || ranked[0].DecisionOutcome != "over_2_5" {
		t.Fatalf("top decision=%+v", ranked[0])
	}
	if ranked[0].DecisionMarketDifference == nil || math.Abs(*ranked[0].DecisionMarketDifference-0.20) > 1e-9 {
		t.Fatalf("top market difference=%v want 0.20", ranked[0].DecisionMarketDifference)
	}
	if math.Abs(ranked[0].DecisionProbability-0.75) > 1e-9 {
		t.Fatalf("top decision probability=%v want 0.75", ranked[0].DecisionProbability)
	}
}

func TestRankPredictionsFallsBackToModelProbability(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, Probability: probability{Home: 0.58, Draw: 0.22, Away: 0.20}},
		{MatchID: 2, Probability: probability{Home: 0.64, Draw: 0.19, Away: 0.17}},
	}

	ranked := rankPredictions(items)
	if ranked[0].MatchID != 2 {
		t.Fatalf("top match=%d want 2", ranked[0].MatchID)
	}
	if ranked[0].DecisionBasis != "model_probability" || ranked[0].DecisionOutcome != "home" {
		t.Fatalf("fallback decision=%+v", ranked[0])
	}
}

func TestTopRankedPredictionKeepsOnlyRankOne(t *testing.T) {
	items := []predictionRow{
		{MatchID: 1, Probability: probability{Home: 0.60, Draw: 0.20, Away: 0.20}},
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
			Home: 0.463,
			Draw: 0.246,
			Away: 0.292,
		},
		PointInTimeVerified: true,
	}
	payload, err := json.Marshal(predictionPayload{
		Bookmaker: &bookmakerPayload{
			Implied: probability{
				Home: 0.423,
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
	if ranked[0].DecisionBasis != "goal_market_difference" {
		t.Fatalf("decision basis=%s want goal_market_difference", ranked[0].DecisionBasis)
	}
	if ranked[0].DecisionOutcome != "over_2_5" {
		t.Fatalf("decision outcome=%s want over_2_5", ranked[0].DecisionOutcome)
	}
	if ranked[0].DecisionMarketDifference == nil || math.Abs(*ranked[0].DecisionMarketDifference-0.07) > 1e-9 {
		t.Fatalf("decision market difference=%v want 0.07", ranked[0].DecisionMarketDifference)
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

func TestDetermineMartingaleStake(t *testing.T) {
	tests := []struct {
		name          string
		baseStake     float64
		settledProfit float64
		pendingStake  float64
		quotedOdds    float64
		want          float64
	}{
		{
			name:       "fresh cycle starts at base stake",
			baseStake:  5000,
			quotedOdds: 1.30,
			want:       5000,
		},
		{
			name:       "minimum odds are accepted",
			baseStake:  50,
			quotedOdds: 1.20,
			want:       50,
		},
		{
			name:          "recovers settled loss and preserves base-stake profit",
			baseStake:     5000,
			settledProfit: -5000,
			quotedOdds:    1.30,
			want:          21666.67,
		},
		{
			name:         "includes unsettled exposure",
			baseStake:    50,
			pendingStake: 50,
			quotedOdds:   1.20,
			want:         300,
		},
		{
			name:          "positive settled profit resets losses",
			baseStake:     50,
			settledProfit: 15,
			quotedOdds:    1.30,
			want:          50,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := determineMartingaleStake(tc.baseStake, tc.settledProfit, tc.pendingStake, tc.quotedOdds)
			if err != nil {
				t.Fatalf("determineMartingaleStake error: %v", err)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("determineMartingaleStake=%v want %v", got, tc.want)
			}
		})
	}
}

func TestDetermineMartingaleStakeRejectsOddsBelowMinimum(t *testing.T) {
	if _, err := determineMartingaleStake(50, 0, 0, 1.19); err == nil {
		t.Fatal("expected odds below 1.20 to be rejected")
	}
}
