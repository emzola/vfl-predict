package api

import (
	"encoding/json"
	"math"
	"testing"
)

func TestApplyPredictionPayloadExposesBookmakerProvenance(t *testing.T) {
	item := predictionView{Probability: probabilityView{Home: 0.55, Draw: 0.25, Away: 0.20}}
	payload, err := json.Marshal(map[string]any{
		"bookmaker": map[string]any{
			"home": 2.0, "draw": 3.0, "away": 4.0,
			"implied":   map[string]float64{"home": 0.5, "draw": 0.3, "away": 0.2},
			"overround": 1.08,
		},
		"secondary":      map[string]any{"over_2_5": 0.51},
		"feature_values": map[string]float64{"home_form": 0.7},
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
	if item.Secondary["over_2_5"] != float64(0.51) || item.FeatureValues["home_form"] != 0.7 {
		t.Fatalf("derived payload secondary=%v features=%v", item.Secondary, item.FeatureValues)
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
