package autobet

import "testing"

func TestRankPredictionsPrefersPositiveMarketDifference(t *testing.T) {
	items := []predictionRow{
		{
			MatchID: 1,
			Probability: probability{
				Home: 0.60, Draw: 0.20, Away: 0.20,
			},
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{ModelDifference: probability{Home: 0.02, Draw: -0.01, Away: 0}},
		},
		{
			MatchID: 2,
			Probability: probability{
				Home: 0.30, Draw: 0.20, Away: 0.50,
			},
			PointInTimeVerified: true,
			Bookmaker: &bookmakerPayload{ModelDifference: probability{Home: 0.01, Draw: 0.02, Away: 0.12}},
		},
	}

	ranked := rankPredictions(items)
	if len(ranked) != 2 {
		t.Fatalf("ranked count=%d want 2", len(ranked))
	}
	if ranked[0].MatchID != 2 {
		t.Fatalf("top match=%d want 2", ranked[0].MatchID)
	}
	if ranked[0].DecisionBasis != "market_difference" || ranked[0].DecisionOutcome != "away" {
		t.Fatalf("top decision=%+v", ranked[0])
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

func TestRoundMoneyRoundsToTwoDecimals(t *testing.T) {
	if got := roundMoney(1234.567); got != 1234.57 {
		t.Fatalf("roundMoney=%v want 1234.57", got)
	}
}
