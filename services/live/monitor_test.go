package live

import (
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/provider"
)

func TestPollPlanUsesFixedInterval(t *testing.T) {
	cfg := DefaultPollConfig()
	if cfg.ResultPollInterval != 5*time.Second {
		t.Fatalf("result poll interval=%s want 5s", cfg.ResultPollInterval)
	}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, plan := range []PollPlan{
		{Now: now, Config: cfg},
		{Now: now, NextKickoff: now.Add(4 * time.Minute), Config: cfg},
		{Now: now, RecentlyCompleted: true, ProviderBackoff: 20 * time.Minute, Config: cfg},
	} {
		if got := plan.NextDelay(); got != cfg.PollInterval {
			t.Fatalf("poll delay=%s want %s", got, cfg.PollInterval)
		}
	}
}

func TestFixtureResultsBaseURLUsesDedicatedJohannesburgFeed(t *testing.T) {
	cfg := ingest.DefaultConfig()
	if got := fixtureResultsBaseURL(cfg); got != ingest.DefaultResultsBaseURL {
		t.Fatalf("results base URL=%q want %q", got, ingest.DefaultResultsBaseURL)
	}
	cfg.ResultsBaseURL = "https://results.example.test/feed/"
	if got := fixtureResultsBaseURL(cfg); got != cfg.ResultsBaseURL {
		t.Fatalf("custom results base URL=%q want %q", got, cfg.ResultsBaseURL)
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
		// An unresolved past fixture is not a future prediction target and must
		// not prevent round 3 from being selected.
		{Round: 1, ScheduledAt: now.Add(-30 * time.Minute), Score: nil},
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
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	seasons := provider.SeasonDiscovery{Seasons: []provider.SeasonSummary{
		{ID: provider.SeasonID("3149001"), TournamentID: 14149, Current: true},
		{ID: provider.SeasonID("3149000"), TournamentID: 14149, Current: false},
	}}
	got, err := currentSeason(seasons, now)
	if err != nil || got.ID != provider.SeasonID("3149001") {
		t.Fatalf("current season=%+v err=%v", got, err)
	}
	seasons.Seasons[1].Current = true
	if _, err := currentSeason(seasons, now); err == nil {
		t.Fatal("multiple current seasons should be rejected")
	}
	seasons.Seasons[1].Current = false
	seasons.Seasons[0].Current = false
	if _, err := currentSeason(seasons, now); err == nil {
		t.Fatal("missing current season should be rejected")
	}
}

func TestCurrentSeasonUsesActiveDateWindowWhenProviderFlagIsStale(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 52, 25, 0, time.UTC)
	seasons := provider.SeasonDiscovery{Seasons: []provider.SeasonSummary{
		{
			ID:           provider.SeasonID("3150118"),
			TournamentID: 14149,
			Current:      true,
			StartUTC:     now.Add(-2 * time.Hour),
			EndUTC:       now.Add(-10 * time.Minute),
		},
		{
			ID:           provider.SeasonID("3150148"),
			TournamentID: 14149,
			Current:      false,
			StartUTC:     now.Add(-10 * time.Minute),
			EndUTC:       now.Add(2 * time.Hour),
		},
	}}

	got, err := currentSeason(seasons, now)
	if err != nil {
		t.Fatalf("current season error: %v", err)
	}
	if got.ID != provider.SeasonID("3150148") {
		t.Fatalf("current season=%s, want 3150148", got.ID)
	}
}

func TestCurrentSeasonPreservesFlagDuringGapBetweenSeasons(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 40, 0, 0, time.UTC)
	seasons := provider.SeasonDiscovery{Seasons: []provider.SeasonSummary{
		{
			ID:           provider.SeasonID("3150118"),
			TournamentID: 14149,
			Current:      true,
			StartUTC:     now.Add(-2 * time.Hour),
			EndUTC:       now.Add(-time.Minute),
		},
		{
			ID:           provider.SeasonID("3150148"),
			TournamentID: 14149,
			Current:      false,
			StartUTC:     now.Add(time.Minute),
			EndUTC:       now.Add(2 * time.Hour),
		},
	}}

	got, err := currentSeason(seasons, now)
	if err != nil {
		t.Fatalf("current season error: %v", err)
	}
	if got.ID != provider.SeasonID("3150118") {
		t.Fatalf("current season=%s, want flagged season during gap", got.ID)
	}
}

func TestCurrentSeasonRejectsMultipleDateActiveSeasons(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 52, 25, 0, time.UTC)
	seasons := provider.SeasonDiscovery{Seasons: []provider.SeasonSummary{
		{
			ID:           provider.SeasonID("3150118"),
			TournamentID: 14149,
			Current:      true,
			StartUTC:     now.Add(-2 * time.Hour),
			EndUTC:       now.Add(-10 * time.Minute),
		},
		{
			ID:           provider.SeasonID("3150148"),
			TournamentID: 14149,
			StartUTC:     now.Add(-10 * time.Minute),
			EndUTC:       now.Add(2 * time.Hour),
		},
		{
			ID:           provider.SeasonID("3150149"),
			TournamentID: 14149,
			StartUTC:     now.Add(-5 * time.Minute),
			EndUTC:       now.Add(3 * time.Hour),
		},
	}}

	if _, err := currentSeason(seasons, now); err == nil {
		t.Fatal("multiple date-active seasons should be rejected")
	}
}
