package ingest

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func TestPostgresIntegrationIdempotencyAndInterruptedRecovery(t *testing.T) {
	databaseURL := os.Getenv("PHASE1_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set PHASE1_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	db, err := OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ApplyMigrations(ctx, db, filepathForIntegrationMigrations(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seasonID := provider.SeasonID("99" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	season := provider.SeasonSummary{
		ID:           seasonID,
		TournamentID: provider.TournamentID(14149),
		Name:         "integration-test",
		StartUTC:     now.Add(-time.Hour),
		EndUTC:       now.Add(time.Hour),
	}
	if err := repo.UpsertSeason(ctx, season, "", now); err != nil {
		t.Fatal(err)
	}
	jobKey := "integration-idempotency:" + seasonID.String()
	jobID1, status1, _, err := repo.EnsureJob(ctx, "season_backfill", seasonID.String(), 0, jobKey, 3)
	if err != nil {
		t.Fatal(err)
	}
	jobID2, status2, _, err := repo.EnsureJob(ctx, "season_backfill", seasonID.String(), 0, jobKey, 3)
	if err != nil {
		t.Fatal(err)
	}
	if jobID1 != jobID2 || status1 != status2 {
		t.Fatalf("idempotent job = %s/%s then %s/%s", jobID1, status1, jobID2, status2)
	}
	checkpointKey := "integration-checkpoint:" + seasonID.String()
	if err := repo.UpsertCheckpoint(ctx, Checkpoint{Key: checkpointKey, State: "running", Attempt: 1, RequestCount: 1, ExpectedCount: 8}, "season_odds", seasonID.String(), 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertCheckpoint(ctx, Checkpoint{Key: checkpointKey, State: "completed", Attempt: 1, RequestCount: 1, ExpectedCount: 8, AcceptedCount: 1}, "season_odds", seasonID.String(), 1); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := repo.GetCheckpoint(ctx, checkpointKey)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.State != "completed" || checkpoint.AcceptedCount != 1 {
		t.Fatalf("recovered checkpoint = %#v", checkpoint)
	}

	response := &Response{StatusCode: 200, FetchedAt: now, ContentType: "application/json"}
	payload := []byte(`{"queryUrl":"integration","doc":[{"data":{"seasons":[]}}]}`)
	rawID1, err := repo.SaveRawPayload(ctx, RawPayloadInput{EndpointFamily: "integration", RequestKey: jobKey, RequestURL: "https://example.invalid/integration", Response: response, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	rawID2, err := repo.SaveRawPayload(ctx, RawPayloadInput{EndpointFamily: "integration", RequestKey: jobKey, RequestURL: "https://example.invalid/integration", Response: response, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if rawID1 != rawID2 {
		t.Fatalf("raw payload IDs differ on retry: %s vs %s", rawID1, rawID2)
	}
	if _, err := repo.LatestRawPayload(ctx, "integration", jobKey); err != nil {
		t.Fatal(err)
	}
	malformedID, err := repo.SaveRawPayload(ctx, RawPayloadInput{
		EndpointFamily: "integration-malformed",
		RequestKey:     jobKey,
		RequestURL:     "https://example.invalid/integration-malformed",
		Response:       response,
		Payload:        []byte("not-json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payloadText string
	var payloadJSON sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT payload_text, payload::text FROM raw_payloads WHERE raw_payload_id = $1::uuid`, malformedID).Scan(&payloadText, &payloadJSON); err != nil {
		t.Fatal(err)
	}
	if payloadText != "not-json" || payloadJSON.Valid {
		t.Fatalf("malformed raw payload was not preserved safely: text=%q json=%#v", payloadText, payloadJSON)
	}

	matchID := provider.MatchID(700000000 + uint64(now.UnixNano()%100_000_000))
	home := provider.TeamRef{UID: provider.TeamUID(7001), ProviderID: 9001, Name: "Integration Home"}
	away := provider.TeamRef{UID: provider.TeamUID(7002), ProviderID: 9002, Name: "Integration Away"}
	fixture := provider.Fixture{
		MatchID:         matchID,
		ProviderMatchID: matchID.String(),
		SeasonID:        seasonID,
		TournamentID:    provider.TournamentID(14149),
		Round:           provider.RoundNumber(1),
		ScheduledUTS:    now.Add(-2 * time.Hour).Unix(),
		ScheduledAt:     now.Add(-2 * time.Hour),
		Home:            home,
		Away:            away,
		Score:           &provider.Score{Home: 1, Away: 0},
	}
	if err := repo.UpsertTeam(ctx, home, rawID1, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTeam(ctx, away, rawID1, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertFixture(ctx, fixture, rawID1, now, 0); err != nil {
		t.Fatal(err)
	}
	decimalOdds, err := provider.ParseDecimal("1.50")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := provider.OddsSnapshot{
		QueryURL:              "integration",
		SourceTimestampMillis: now.UnixMilli(),
		SourceTimestampUTC:    now,
		Records: []provider.OddsRecord{{
			MatchID:         fixture.MatchID,
			ProviderMatchID: fixture.ProviderMatchID,
			TournamentID:    provider.TournamentID(14149),
			HomeTeamUID:     home.UID,
			AwayTeamUID:     away.UID,
			Markets: []provider.Market{{
				ID:         1,
				Name:       "1x2",
				Specifiers: "",
				Status:     -3,
				Outcomes: []provider.Outcome{{
					ID:             "1",
					Name:           "Integration Home",
					RawOdds:        "1.50",
					Odds:           &decimalOdds,
					Active:         0,
					ProviderResult: 1,
				}},
			}},
		}},
	}
	fixtureMap := map[provider.MatchID]provider.Fixture{fixture.MatchID: fixture}
	if _, err := repo.UpsertOddsSnapshot(ctx, snapshot, fixtureMap, rawID1, now, "historical_returned_after_settlement", `{"test":true}`, jobID1, checkpointKey); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertOddsSnapshot(ctx, snapshot, fixtureMap, rawID1, now, "historical_returned_after_settlement", `{"test":true}`, jobID1, checkpointKey); err != nil {
		t.Fatal(err)
	}
	var snapshotCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM odds_snapshots WHERE match_id = $1`, matchID).Scan(&snapshotCount); err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 1 {
		t.Fatalf("normalized odds rows after retry = %d, want 1", snapshotCount)
	}
}

func filepathForIntegrationMigrations(t *testing.T) string {
	t.Helper()
	return "../../db/migrations"
}
