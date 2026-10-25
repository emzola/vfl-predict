package prediction

import (
	"testing"
	"time"

	"bet9ja-vfl/services/research"
)

func TestEligibleOddsEnforcesVerifiedObservationCutoff(t *testing.T) {
	cutoff := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	quotes := []research.OddsQuote{
		{MarketID: 1, Odds: 2.1, PointInTimeVerified: true, ObservedAt: cutoff.Add(-time.Minute)},
		{MarketID: 1, Odds: 3.2, PointInTimeVerified: true, ObservedAt: cutoff.Add(time.Second)},
		{MarketID: 1, Odds: 4.3, PointInTimeVerified: false, ObservedAt: cutoff.Add(-time.Minute)},
	}
	got := eligibleOdds(quotes, cutoff, true)
	if len(got) != 1 || got[0].Odds != 2.1 {
		t.Fatalf("eligible odds=%+v", got)
	}
}

func TestDefaultPredictionPolicyUsesSafetyBuffer(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SafetyBuffer != 30*time.Second || cfg.RequireVerifiedOdds != true {
		t.Fatalf("unexpected default policy: %+v", cfg)
	}
}
