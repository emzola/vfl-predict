package provider

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func readProviderFixture(t *testing.T, name string) []byte {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "fixtures", "provider", name)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return payload
}

func TestParseCompletedFixturesGolden(t *testing.T) {
	payload, err := ParseFixtures(readProviderFixture(t, "completed-season-fixtures.json"))
	if err != nil {
		t.Fatalf("parse completed fixtures: %v", err)
	}
	if payload.QueryURL != "stats_season_fixtures2/3149311" {
		t.Fatalf("query URL = %q", payload.QueryURL)
	}
	if payload.Season.ID != "3149311" {
		t.Fatalf("season ID = %q", payload.Season.ID)
	}
	if payload.Season.TournamentID != TournamentID(14149) {
		t.Fatalf("tournament ID = %s", payload.Season.TournamentID)
	}
	if payload.Season.CurrentSeasonID == nil || *payload.Season.CurrentSeasonID != SeasonID("3149336") {
		t.Fatalf("current season ID = %v", payload.Season.CurrentSeasonID)
	}
	if len(payload.Matches) != 240 {
		t.Fatalf("match count = %d, want 240", len(payload.Matches))
	}

	rounds := make(map[RoundNumber]int)
	teams := make(map[TeamUID]struct{})
	ids := make(map[MatchID]struct{})
	for _, match := range payload.Matches {
		rounds[match.Round]++
		ids[match.MatchID] = struct{}{}
		teams[match.Home.UID] = struct{}{}
		teams[match.Away.UID] = struct{}{}
		if match.Score == nil || match.FullTimeScore == nil || match.HalfTimeScore == nil {
			t.Fatalf("match %s is missing a score or period score", match.MatchID)
		}
		if match.PeriodsKind != PeriodsObject {
			t.Fatalf("match %s periods kind = %s", match.MatchID, match.PeriodsKind)
		}
	}
	if len(rounds) != 30 || len(ids) != 240 || len(teams) != 16 {
		t.Fatalf("shape = rounds:%d matches:%d teams:%d, want 30/240/16", len(rounds), len(ids), len(teams))
	}
	for round := RoundNumber(1); round <= 30; round++ {
		if rounds[round] != 8 {
			t.Fatalf("round %d match count = %d, want 8", round, rounds[round])
		}
	}

	first := payload.Matches[0]
	if first.ScheduledAt != time.Unix(1792852734, 0).UTC() {
		t.Fatalf("first kickoff = %s", first.ScheduledAt)
	}
	if got := first.StatusAsOf(time.Unix(1792870000, 0), 120*time.Second); got != MatchStatusCompleted {
		t.Fatalf("completed status = %s", got)
	}
}

func TestParseUpcomingFixturesPreservesFutureNullsGolden(t *testing.T) {
	payload, err := ParseFixtures(readProviderFixture(t, "upcoming-matchdays.json"))
	if err != nil {
		t.Fatalf("parse upcoming fixtures: %v", err)
	}
	if payload.Season.ID != "3149369" {
		t.Fatalf("season ID = %q", payload.Season.ID)
	}
	if len(payload.Matches) != 240 {
		t.Fatalf("match count = %d, want 240", len(payload.Matches))
	}
	var scored, future, emptyPeriodArrays int
	for _, match := range payload.Matches {
		if match.Score == nil {
			future++
			if match.PeriodsKind != PeriodsEmptyArray {
				t.Fatalf("future match %s periods kind = %s, want empty_array", match.MatchID, match.PeriodsKind)
			}
			emptyPeriodArrays++
			if got := match.StatusAsOf(match.ScheduledAt.Add(-time.Minute), 120*time.Second); got != MatchStatusScheduled {
				t.Fatalf("future match %s status = %s, want scheduled", match.MatchID, got)
			}
		} else {
			scored++
			if match.PeriodsKind != PeriodsObject {
				t.Fatalf("scored match %s periods kind = %s, want object", match.MatchID, match.PeriodsKind)
			}
		}
	}
	if scored != 160 || future != 80 || emptyPeriodArrays != 80 {
		t.Fatalf("scored/future/empty periods = %d/%d/%d, want 160/80/80", scored, future, emptyPeriodArrays)
	}
}

func TestParseOddsGolden(t *testing.T) {
	payload, err := ParseOdds(readProviderFixture(t, "historical-match-odds.json"))
	if err != nil {
		t.Fatalf("parse odds: %v", err)
	}
	if payload.QueryURL != "stats_season_odds2/vf:season:3149336/23" {
		t.Fatalf("query URL = %q", payload.QueryURL)
	}
	if payload.SourceTimestampMillis != 1792864076774 {
		t.Fatalf("source timestamp = %d", payload.SourceTimestampMillis)
	}
	if payload.SourceTimestampUTC != time.UnixMilli(1792864076774).UTC() {
		t.Fatalf("source UTC timestamp = %s", payload.SourceTimestampUTC)
	}
	if len(payload.Records) != 8 {
		t.Fatalf("odds record count = %d, want 8", len(payload.Records))
	}

	var marketCount, outcomeCount, inactiveOutcomes, inactiveMarkets, nullSpecifiers int
	identities := make(map[MarketIdentity]struct{})
	for _, record := range payload.Records {
		if record.TournamentID != TournamentID(14149) {
			t.Fatalf("record %s tournament ID = %s", record.MatchID, record.TournamentID)
		}
		if record.HomeTeamUID == 0 || record.AwayTeamUID == 0 {
			t.Fatalf("record %s has zero team UID", record.MatchID)
		}
		for _, market := range record.Markets {
			marketCount++
			if !market.IsActive() {
				inactiveMarkets++
			}
			if market.ProviderSpecifiers == nil {
				nullSpecifiers++
			}
			for _, outcome := range market.Outcomes {
				outcomeCount++
				if !outcome.IsActive() {
					inactiveOutcomes++
				}
				if outcome.ProviderResult != -1 {
					t.Fatalf("outcome %s result = %d, want source value -1", outcome.ID, outcome.ProviderResult)
				}
				if outcome.Odds == nil {
					if outcome.OddsError == "" {
						t.Fatalf("outcome %s has nil odds without a validation error", outcome.ID)
					}
				} else if outcome.Odds.String() == "" || outcome.Odds.IsZero() {
					t.Fatalf("outcome %s has empty/zero odds", outcome.ID)
				}
				identity := market.Identity(record.MatchID, outcome)
				if _, exists := identities[identity]; exists {
					t.Fatalf("duplicate market identity %s", identity)
				}
				identities[identity] = struct{}{}
			}
		}
	}
	if marketCount != 346 || outcomeCount != 1116 || len(identities) != 1116 {
		t.Fatalf("markets/outcomes/identities = %d/%d/%d, want 346/1116/1116", marketCount, outcomeCount, len(identities))
	}
	if inactiveOutcomes != 11 || inactiveMarkets != 4 || nullSpecifiers != 80 {
		t.Fatalf("inactive outcomes/markets/null specifiers = %d/%d/%d, want 11/4/80", inactiveOutcomes, inactiveMarkets, nullSpecifiers)
	}
	if len(payload.ValidationIssues) != 1 || payload.ValidationIssues[0].RawOdds != "0.00" {
		t.Fatalf("odds validation issues = %#v, want the one observed zero-odds outcome", payload.ValidationIssues)
	}
}

func TestParseSeasonDiscoveryGolden(t *testing.T) {
	payload, err := ParseSeasonDiscovery(readProviderFixture(t, "historical-seasons.json"))
	if err != nil {
		t.Fatalf("parse season discovery: %v", err)
	}
	if payload.QueryURL != "uniquetournament_seasons/14149" {
		t.Fatalf("query URL = %q", payload.QueryURL)
	}
	if len(payload.Seasons) != 169 {
		t.Fatalf("season count = %d, want 169", len(payload.Seasons))
	}
	current := 0
	for _, season := range payload.Seasons {
		if season.Current {
			current++
			if season.ID != SeasonID("3149336") {
				t.Fatalf("current season ID = %s, want 3149336", season.ID)
			}
		}
		if season.StartUTC.IsZero() || season.EndUTC.IsZero() {
			t.Fatalf("season %s has missing normalized dates", season.ID)
		}
	}
	if current != 1 {
		t.Fatalf("current season count = %d, want 1", current)
	}
}

func TestOddsAndFixtureSamplesHaveNoJoinableIDs(t *testing.T) {
	odds, err := ParseOdds(readProviderFixture(t, "historical-match-odds.json"))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := ParseFixtures(readProviderFixture(t, "completed-season-fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	upcoming, err := ParseFixtures(readProviderFixture(t, "upcoming-matchdays.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureIDs := make(map[MatchID]struct{}, len(completed.Matches)+len(upcoming.Matches))
	for _, match := range completed.Matches {
		fixtureIDs[match.MatchID] = struct{}{}
	}
	for _, match := range upcoming.Matches {
		fixtureIDs[match.MatchID] = struct{}{}
	}
	for _, record := range odds.Records {
		if _, exists := fixtureIDs[record.MatchID]; exists {
			t.Fatalf("sample odds match %s unexpectedly joins supplied fixture samples", record.MatchID)
		}
	}
}
