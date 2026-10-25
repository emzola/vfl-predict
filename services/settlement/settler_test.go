package settlement

import "testing"

func TestSettlementOutcomeAndArgmax(t *testing.T) {
	if got := argmax(map[string]float64{"home": 0.2, "draw": 0.5, "away": 0.3}); got != "draw" {
		t.Fatalf("argmax=%s", got)
	}
	if got := classify("3", "Draw", "Alpha", "Beta"); got != "draw" {
		t.Fatalf("classify=%s", got)
	}
	if got := classify("2", "Beta", "Alpha", "Beta"); got != "away" {
		t.Fatalf("classify=%s", got)
	}
	actual, logLoss, brier, correct := ComputePredictionMetrics(0.6, 0.2, 0.2, 2, 0)
	if actual != "home" || !correct || logLoss <= 0 || brier <= 0 {
		t.Fatalf("metrics actual=%s logloss=%v brier=%v correct=%t", actual, logLoss, brier, correct)
	}
}
