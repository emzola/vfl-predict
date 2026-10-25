package api

import (
	"testing"
	"time"
)

func TestNextUpcomingRoundSkipsInProgressRound(t *testing.T) {
	now := time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC)
	inProgressKickoff := now.Add(-30 * time.Minute)
	nextKickoff := now.Add(2 * time.Hour)

	selected := nextUpcomingRound([]roundView{
		{Round: 12, State: "provisional", EarliestKickoffAt: &inProgressKickoff},
		{Round: 13, State: "provisional", EarliestKickoffAt: &nextKickoff},
	}, now)

	if selected == nil {
		t.Fatal("expected the next upcoming round")
	}
	if selected.Round != 13 {
		t.Fatalf("selected round=%d want 13", selected.Round)
	}
}

func TestNextUpcomingRoundReturnsNilWhenNoFutureRoundExists(t *testing.T) {
	now := time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC)
	pastKickoff := now.Add(-time.Minute)

	selected := nextUpcomingRound([]roundView{
		{Round: 12, State: "settled", EarliestKickoffAt: &pastKickoff},
		{Round: 13, State: "provisional", EarliestKickoffAt: &pastKickoff},
	}, now)

	if selected != nil {
		t.Fatalf("selected round=%d, want nil", selected.Round)
	}
}

func TestNextUpcomingRoundIncludesRoundStartingNow(t *testing.T) {
	now := time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC)

	selected := nextUpcomingRound([]roundView{
		{Round: 14, State: "provisional", EarliestKickoffAt: &now},
	}, now)

	if selected == nil || selected.Round != 14 {
		if selected == nil {
			t.Fatal("expected the round starting now")
		}
		t.Fatalf("selected round=%d want 14", selected.Round)
	}
}
