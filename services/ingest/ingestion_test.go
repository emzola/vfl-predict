package ingest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "..", "..", "fixtures", "provider", name))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestValidateFixtureCoverageAndGoldenJoinBoundary(t *testing.T) {
	fixtures, err := provider.ParseFixtures(readFixture(t, "completed-season-fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ValidateFixtureCoverage(fixtures.Matches); len(got) != 0 {
		t.Fatalf("fixture coverage errors = %v", got)
	}
	odds, err := provider.ParseOdds(readFixture(t, "historical-match-odds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ValidateOddsJoins(fixtures.Matches, odds); len(got) != len(odds.Records) {
		t.Fatalf("supplied mismatched golden join errors = %d, want %d", len(got), len(odds.Records))
	}
	joinedFixture := provider.Fixture{
		MatchID: fixtures.Matches[0].MatchID,
		Home:    provider.TeamRef{UID: odds.Records[0].HomeTeamUID},
		Away:    provider.TeamRef{UID: odds.Records[0].AwayTeamUID},
	}
	joinedOdds := odds.Records[0]
	joinedOdds.MatchID = joinedFixture.MatchID
	if got := ValidateOddsJoins([]provider.Fixture{joinedFixture}, provider.OddsSnapshot{Records: []provider.OddsRecord{joinedOdds}}); len(got) != 0 {
		t.Fatalf("normalized join errors = %v", got)
	}
}

func TestSelectSeasonsIsBoundedAndExcludesCurrent(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	seasons := provider.SeasonDiscovery{Seasons: []provider.SeasonSummary{
		{ID: "older", StartUTC: now.Add(-72 * time.Hour), EndUTC: now.Add(-48 * time.Hour)},
		{ID: "newer", StartUTC: now.Add(-24 * time.Hour), EndUTC: now.Add(-time.Hour)},
		{ID: "current", Current: true, StartUTC: now.Add(-time.Hour), EndUTC: now.Add(time.Hour)},
	}}
	selected, err := selectSeasons(seasons, "", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].ID != "newer" {
		t.Fatalf("selected seasons = %#v", selected)
	}
	if _, err := selectSeasons(seasons, "current", 1, now); err == nil {
		t.Fatal("current season was selectable as completed")
	}
}

func TestValidateOddsJoinsRejectsTeamMismatch(t *testing.T) {
	fixture := provider.Fixture{MatchID: provider.MatchID(7), Home: provider.TeamRef{UID: 1}, Away: provider.TeamRef{UID: 2}}
	odds := provider.OddsSnapshot{Records: []provider.OddsRecord{{MatchID: provider.MatchID(7), HomeTeamUID: 1, AwayTeamUID: 3}}}
	if got := ValidateOddsJoins([]provider.Fixture{fixture}, odds); len(got) != 1 {
		t.Fatalf("join errors = %v, want one", got)
	}
}
