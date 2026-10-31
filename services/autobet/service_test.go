package autobet

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRankPredictionsPrefersPositiveMarketDifference(t *testing.T) {
	items := []predictionRow{
		{
			MatchID: 1,
			Probability: probability{
				Home: 0.60, Draw: 0.20, Away: 0.20,
			},
			PointInTimeVerified: true,
			Bookmaker:           &bookmakerPayload{ModelDifference: probability{Home: 0.02, Draw: -0.01, Away: 0}},
		},
		{
			MatchID: 2,
			Probability: probability{
				Home: 0.30, Draw: 0.20, Away: 0.50,
			},
			PointInTimeVerified: true,
			Bookmaker:           &bookmakerPayload{ModelDifference: probability{Home: 0.01, Draw: 0.02, Away: 0.12}},
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
	if ranked[0].DecisionBasis != "market_difference" {
		t.Fatalf("decision basis=%s want market_difference", ranked[0].DecisionBasis)
	}
	if ranked[0].DecisionOutcome != "home" {
		t.Fatalf("decision outcome=%s want home", ranked[0].DecisionOutcome)
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

	if !qualifiesForSecondary(item.Secondary) {
		t.Fatalf("secondary payload should qualify: %+v", item.Secondary)
	}
}

func TestRoundMoneyRoundsToTwoDecimals(t *testing.T) {
	if got := roundMoney(1234.567); got != 1234.57 {
		t.Fatalf("roundMoney=%v want 1234.57", got)
	}
}

func TestQualifiesForOver15(t *testing.T) {
	tests := []struct {
		name  string
		value probability
		want  bool
	}{
		{
			name:  "home and draw both exceed away",
			value: probability{Home: 0.855, Draw: 0.097, Away: 0.048},
			want:  true,
		},
		{
			name:  "away and draw both exceed home",
			value: probability{Home: 0.12, Draw: 0.28, Away: 0.60},
			want:  true,
		},
		{
			name:  "draw does not exceed away",
			value: probability{Home: 0.39, Draw: 0.264, Away: 0.346},
			want:  false,
		},
		{
			name:  "ties do not qualify",
			value: probability{Home: 0.40, Draw: 0.30, Away: 0.40},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := qualifiesForOver15(tc.value); got != tc.want {
				t.Fatalf("qualifiesForOver15(%+v)=%v want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestQualifiesForSecondary(t *testing.T) {
	valid := &secondaryGoalPayload{
		Over25:           0.74,
		Under25:          0.26,
		BothTeamsToScore: 0.52,
		NeitherTeamScore: 0.48,
	}
	tests := []struct {
		name  string
		value *secondaryGoalPayload
		want  bool
	}{
		{name: "over 2.5 is highest and btts is second", value: valid, want: true},
		{
			name:  "over 2.5 does not exceed btts",
			value: &secondaryGoalPayload{Over25: 0.52, Under25: 0.20, BothTeamsToScore: 0.60, NeitherTeamScore: 0.40},
			want:  false,
		},
		{
			name:  "btts does not exceed under 2.5",
			value: &secondaryGoalPayload{Over25: 0.74, Under25: 0.55, BothTeamsToScore: 0.52, NeitherTeamScore: 0.48},
			want:  false,
		},
		{
			name:  "btts does not exceed neither",
			value: &secondaryGoalPayload{Over25: 0.74, Under25: 0.26, BothTeamsToScore: 0.48, NeitherTeamScore: 0.52},
			want:  false,
		},
		{name: "missing secondary market", value: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := qualifiesForSecondary(tc.value); got != tc.want {
				t.Fatalf("qualifiesForSecondary(%+v)=%v want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestDetermineNextStake(t *testing.T) {
	tests := []struct {
		name      string
		baseStake float64
		want      float64
	}{
		{
			name:      "flat stake remains unchanged",
			baseStake: 5000,
			want:      5000,
		},
		{
			name:      "decimal stake is rounded",
			baseStake: 5000.567,
			want:      5000.57,
		},
		{
			name:      "fresh cycle uses base stake",
			baseStake: 5000,
			want:      5000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := determineNextStake(tc.baseStake)
			if got != tc.want {
				t.Fatalf("determineNextStake=%v want %v", got, tc.want)
			}
		})
	}
}
