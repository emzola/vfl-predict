package live

import (
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func TestPollPlanUsesConservativeAdaptiveIntervals(t *testing.T) {
	cfg := DefaultPollConfig()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if got := (PollPlan{Now: now, Config: cfg}).NextDelay(); got != cfg.FutureOddsInterval {
		t.Fatalf("no-imminent delay=%s want %s", got, cfg.FutureOddsInterval)
	}
	if got := (PollPlan{Now: now, NextKickoff: now.Add(4 * time.Minute), Config: cfg}).NextDelay(); got != cfg.UpcomingOddsInterval {
		t.Fatalf("imminent delay=%s want %s", got, cfg.UpcomingOddsInterval)
	}
	if got := (PollPlan{Now: now, RecentlyCompleted: true, Config: cfg}).NextDelay(); got != cfg.RecentlyCompleted {
		t.Fatalf("recent completion delay=%s want %s", got, cfg.RecentlyCompleted)
	}
	if got := (PollPlan{Now: now, ProviderBackoff: 20 * time.Minute, Config: cfg}).NextDelay(); got != 20*time.Minute {
		t.Fatalf("provider backoff delay=%s", got)
	}
}

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker(2, time.Minute)
	if !breaker.Allow(now) {
		t.Fatal("new breaker should allow")
	}
	breaker.Failure(now)
	if !breaker.Allow(now) {
		t.Fatal("breaker opened too early")
	}
	breaker.Failure(now)
	if breaker.Allow(now.Add(30 * time.Second)) {
		t.Fatal("breaker should be open")
	}
	if !breaker.Allow(now.Add(time.Minute)) {
		t.Fatal("breaker should permit a half-open retry")
	}
	breaker.Success()
	if !breaker.Allow(now.Add(time.Minute)) {
		t.Fatal("successful retry should close breaker")
	}
}

func TestUpcomingRoundsExcludeCompletedAndOrderByKickoff(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	fixtures := []provider.Fixture{
		{Round: 2, ScheduledAt: now.Add(20 * time.Minute), Score: nil},
		{Round: 2, ScheduledAt: now.Add(25 * time.Minute), Score: nil},
		{Round: 1, ScheduledAt: now.Add(-time.Hour), Score: &provider.Score{Home: 1, Away: 0}},
		{Round: 3, ScheduledAt: now.Add(2 * time.Hour), Score: nil},
	}
	got, first := upcomingRounds(fixtures, now)
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("rounds=%v", got)
	}
	if !first.Equal(now.Add(20 * time.Minute)) {
		t.Fatalf("first kickoff=%s", first)
	}
}

func TestCurrentSeasonRequiresExactlyOneCurrentFlag(t *testing.T) {
	seasons := provider.SeasonDiscovery{Seasons: []provider.SeasonSummary{
		{ID: provider.SeasonID("3149001"), TournamentID: 14149, Current: true},
		{ID: provider.SeasonID("3149000"), TournamentID: 14149, Current: false},
	}}
	got, err := currentSeason(seasons)
	if err != nil || got.ID != provider.SeasonID("3149001") {
		t.Fatalf("current season=%+v err=%v", got, err)
	}
	seasons.Seasons[1].Current = true
	if _, err := currentSeason(seasons); err == nil {
		t.Fatal("multiple current seasons should be rejected")
	}
	seasons.Seasons[1].Current = false
	seasons.Seasons[0].Current = false
	if _, err := currentSeason(seasons); err == nil {
		t.Fatal("missing current season should be rejected")
	}
}
