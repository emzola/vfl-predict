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
	if cfg.ModelName != "bookmaker_first" || cfg.PolicyVersion != "phase5.pre_match.bookmaker_first.v1" || cfg.SafetyBuffer != 30*time.Second || cfg.RequireVerifiedOdds != true {
		t.Fatalf("unexpected default policy: %+v", cfg)
	}
}

func TestBookmakerFirstRequiresCompleteVerifiedLineBeforeUse(t *testing.T) {
	cutoff := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	quotes := []research.OddsQuote{
		{MarketID: 1, OutcomeID: "1", OutcomeName: "Home", Odds: 2.1, PointInTimeVerified: true, ObservedAt: cutoff.Add(-time.Minute)},
		{MarketID: 1, OutcomeID: "3", OutcomeName: "Draw", Odds: 3.2, PointInTimeVerified: true, ObservedAt: cutoff.Add(-time.Minute)},
		{MarketID: 1, OutcomeID: "2", OutcomeName: "Away", Odds: 4.3, PointInTimeVerified: true, ObservedAt: cutoff.Add(time.Second)},
	}
	match := research.MatchRecord{HomeTeamName: "Home", AwayTeamName: "Away", Odds: eligibleOdds(quotes, cutoff, true)}
	if _, ok := research.FindOneXTwoOddsAt(match, cutoff); ok {
		t.Fatal("late outcome completed a pre-cutoff line")
	}
	model := research.NewBookmakerFirstModel(false)
	if model.UsesBookmaker(match) {
		t.Fatal("bookmaker-first accepted an incomplete pre-cutoff line")
	}
	if len(match.Odds) != 2 {
		t.Fatalf("eligible odds=%d want 2", len(match.Odds))
	}
}
