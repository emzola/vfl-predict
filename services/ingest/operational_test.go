package ingest

import (
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func TestLiveOddsPointInTimeVerificationIsBeforeKickoffOnly(t *testing.T) {
	kickoff := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	fixture := provider.Fixture{ScheduledAt: kickoff}
	if !LiveOddsPointInTimeVerified(fixture, kickoff.Add(-time.Second)) {
		t.Fatal("pre-kickoff observation should be verified")
	}
	if LiveOddsPointInTimeVerified(fixture, kickoff) || LiveOddsPointInTimeVerified(fixture, kickoff.Add(time.Second)) {
		t.Fatal("kickoff/post-kickoff observations must not be verified")
	}
}
