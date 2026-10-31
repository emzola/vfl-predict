package api

import (
	"encoding/json"
	"math"
	"testing"
)

func TestApplyPredictionPayloadExposesBookmakerProvenance(t *testing.T) {
	item := predictionView{Probability: probabilityView{Home: 0.55, Draw: 0.25, Away: 0.20}}
	payload, err := json.Marshal(map[string]any{
		"model":             "bookmaker_implied",
		"model_version":     "phase2.v1",
		"prediction_policy": "bookmaker_first",
		"bookmaker": map[string]any{
			"home": 2.0, "draw": 3.0, "away": 4.0,
			"implied":   map[string]float64{"home": 0.5, "draw": 0.3, "away": 0.2},
			"overround": 1.08,
		},
		"bookmaker_goal_market": map[string]any{
			"market_id": 18, "specifiers": "total=2.5",
			"over_odds": 1.70, "under_odds": 2.30,
			"implied":          map[string]float64{"over_2_5": 0.58, "under_2_5": 0.42},
			"model_difference": 0.99,
			"overround":        1.05, "point_in_time_verified": true,
		},
		"secondary":             map[string]any{"over_2_5": 0.51},
		"secondary_model":       "poisson",
		"dynamic_team_strength": map[string]any{"home_rating": 0.42, "total_matches_used": 12},
		"feature_values":        map[string]float64{"home_form": 0.7},
	})
	if err != nil {
		t.Fatal(err)
	}
	applyPredictionPayload(&item, payload)
	if item.Bookmaker == nil || item.Bookmaker.HomeOdds != 2.0 || item.Bookmaker.Implied.Draw != 0.3 {
		t.Fatalf("bookmaker payload=%+v", item.Bookmaker)
	}
	if math.Abs(item.Bookmaker.ModelDifference.Home-0.05) > 1e-9 {
		t.Fatalf("home model difference=%v", item.Bookmaker.ModelDifference.Home)
	}
	if item.BookmakerGoalMarket == nil || item.BookmakerGoalMarket.OverOdds != 1.70 {
		t.Fatalf("goal-market payload=%+v", item.BookmakerGoalMarket)
	}
	if math.Abs(item.BookmakerGoalMarket.ModelDifference-(0.51-0.58)) > 1e-9 {
		t.Fatalf("Over 2.5 model difference=%v", item.BookmakerGoalMarket.ModelDifference)
	}
	if item.Engine != "bookmaker_implied" || item.EngineVersion != "phase2.v1" || item.PredictionPolicy != "bookmaker_first" {
		t.Fatalf("engine provenance=%+v", item)
	}
	if item.Secondary["over_2_5"] != float64(0.51) || item.FeatureValues["home_form"] != 0.7 {
		t.Fatalf("derived payload secondary=%v features=%v", item.Secondary, item.FeatureValues)
	}
	if item.SecondaryModel != "poisson" {
		t.Fatalf("secondary model=%q", item.SecondaryModel)
	}
	if item.DynamicTeamStrength["home_rating"] != float64(0.42) || item.Secondary["dynamic_team_strength"] == nil {
		t.Fatalf("dynamic diagnostics=%v secondary=%v", item.DynamicTeamStrength, item.Secondary)
	}
}

func TestPredictionStatusFilterIsBounded(t *testing.T) {
	for _, value := range []string{"", "pending", "provisional", "frozen", "settled", "stale", "unavailable", "missed_cutoff"} {
		if !validPredictionStatus(value) {
			t.Fatalf("status %q rejected", value)
		}
	}
	for _, value := range []string{"admin", "SELECT", "frozen;drop table"} {
		if validPredictionStatus(value) {
			t.Fatalf("status %q accepted", value)
		}
	}
}
