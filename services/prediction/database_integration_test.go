package prediction

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/provider"
	"bet9ja-vfl/services/research"
)

func TestPhase3DatabaseSchemaAndFrozenGuard(t *testing.T) {
	databaseURL := os.Getenv("PHASE3_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set PHASE3_DATABASE_URL to a dedicated PostgreSQL database")
	}
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"live_feed_observations", "odds_change_history", "odds_snapshot_observations", "round_states", "prediction_runs", "prediction_revisions", "prediction_settlements", "forward_paper_results", "scheduler_leases", "provider_request_budget", "job_execution_history"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("phase 3 table %s is missing", table)
		}
	}
	repo := ingest.NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	cachedRequestKey := "cached-" + strconv.FormatInt(now.UnixNano(), 10)
	response := &ingest.Response{StatusCode: 200, URL: "https://example.invalid/live", Body: []byte(`{"doc":[]}`), FetchedAt: now}
	rawID, err := repo.SaveRawPayload(ctx, ingest.RawPayloadInput{EndpointFamily: "phase3-test", RequestKey: cachedRequestKey, RequestURL: response.URL, Response: response, Payload: response.Body})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveObservation(ctx, "phase3-test", cachedRequestKey, response, rawID, false, "", 0, "accepted", `{}`); err != nil {
		t.Fatal(err)
	}
	response.FetchedAt = now.Add(time.Second)
	if err := repo.SaveObservation(ctx, "phase3-test", cachedRequestKey, response, rawID, true, "", 0, "not_modified", `{}`); err != nil {
		t.Fatal(err)
	}
	var observationCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM live_feed_observations WHERE raw_payload_id = $1::uuid`, rawID).Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if observationCount != 2 {
		t.Fatalf("cached payload observations=%d want 2", observationCount)
	}
	seasonID := provider.SeasonID("93" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	season := provider.SeasonSummary{ID: seasonID, TournamentID: 14149, Name: "phase3-integration", StartUTC: now.Add(-time.Hour), EndUTC: now.Add(time.Hour)}
	if err := repo.UpsertSeason(ctx, season, "", now); err != nil {
		t.Fatal(err)
	}
	home := provider.TeamRef{UID: provider.TeamUID(9301), ProviderID: 9301, Name: "Phase3 Home"}
	away := provider.TeamRef{UID: provider.TeamUID(9302), ProviderID: 9302, Name: "Phase3 Away"}
	if err := repo.UpsertTeam(ctx, home, "", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTeam(ctx, away, "", now); err != nil {
		t.Fatal(err)
	}
	matchID := provider.MatchID(930000000 + uint64(now.UnixNano()%1_000_000))
	fixture := provider.Fixture{MatchID: matchID, ProviderMatchID: matchID.String(), SeasonID: seasonID, TournamentID: 14149, Round: 1, ScheduledUTS: now.Add(time.Hour).Unix(), ScheduledAt: now.Add(time.Hour), Home: home, Away: away}
	if err := repo.UpsertFixture(ctx, fixture, "", now, 0); err != nil {
		t.Fatal(err)
	}
	rawOddsID, err := repo.SaveRawPayload(ctx, ingest.RawPayloadInput{
		EndpointFamily: "phase3-test-odds",
		RequestKey:     "observation-history",
		RequestURL:     "https://example.invalid/odds",
		Response:       &ingest.Response{StatusCode: 200, URL: "https://example.invalid/odds", Body: []byte(`{"doc":[]}`), FetchedAt: now},
		Payload:        []byte(`{"doc":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	homeOdds, _ := provider.ParseDecimal("2.10")
	drawOdds, _ := provider.ParseDecimal("3.20")
	awayOdds, _ := provider.ParseDecimal("3.40")
	oddsSnapshot := provider.OddsSnapshot{
		SourceTimestampMillis: now.UnixMilli(),
		SourceTimestampUTC:    now,
		Records: []provider.OddsRecord{{
			MatchID: matchID, HomeTeamUID: home.UID, AwayTeamUID: away.UID,
			Markets: []provider.Market{{ID: 1, Name: "1x2", Outcomes: []provider.Outcome{
				{ID: "1", Name: home.Name, RawOdds: homeOdds.String(), Odds: &homeOdds, Active: 1},
				{ID: "3", Name: "draw", RawOdds: drawOdds.String(), Odds: &drawOdds, Active: 1},
				{ID: "2", Name: away.Name, RawOdds: awayOdds.String(), Odds: &awayOdds, Active: 1},
			}}},
		}},
	}
	fixtureMap := map[provider.MatchID]provider.Fixture{matchID: fixture}
	if _, err := repo.UpsertLiveOddsSnapshot(ctx, oddsSnapshot, fixtureMap, rawOddsID, now, `{"test":"pre_kickoff"}`, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertLiveOddsSnapshot(ctx, oddsSnapshot, fixtureMap, rawOddsID, now.Add(time.Minute), `{"test":"cached_reobservation"}`, "", ""); err != nil {
		t.Fatal(err)
	}
	var oddsObservationCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM odds_snapshot_observations o JOIN odds_snapshots s ON s.odds_snapshot_id = o.odds_snapshot_id WHERE s.match_id = $1`, matchID).Scan(&oddsObservationCount); err != nil {
		t.Fatal(err)
	}
	if oddsObservationCount != 6 {
		t.Fatalf("odds observations=%d want 6 (two observations for three 1X2 outcomes)", oddsObservationCount)
	}
	modelID, err := research.NewResearchStorage(db).EnsureModelDefinition(ctx, "phase3_integration_model", "test.v1", "integration", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO prediction_runs (season_id, round_no, model_definition_id, model_version, run_type, status, prediction_as_of, input_cutoff_at, earliest_kickoff_at, input_dataset_version, prediction_policy, idempotency_key)
VALUES ($1, 1, $2, 'test.v1', 'frozen', 'frozen', $3, $3, $4, 'integration', 'test', $5)
RETURNING prediction_run_id::text`, seasonID.String(), modelID, now, now.Add(time.Hour), "phase3-run:"+seasonID.String()).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	var revisionID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO prediction_revisions (prediction_run_id, season_id, match_id, round_no, revision_no, status, prediction_as_of, cutoff_at, home_probability, draw_probability, away_probability, eligible, frozen_at)
VALUES ($1::uuid, $2, $3, 1, 1, 'frozen', $4, $5, 0.4, 0.3, 0.3, true, $4)
RETURNING prediction_revision_id::text`, runID, seasonID.String(), matchID, now, now.Add(time.Hour)).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE prediction_revisions SET home_probability = 0.5 WHERE prediction_revision_id = $1::uuid`, revisionID); err == nil {
		t.Fatal("frozen prediction mutation unexpectedly succeeded")
	}
	token, acquired, err := repo.AcquireLease(ctx, "phase3-integration-lease", "test-worker", time.Minute)
	if err != nil || !acquired || token == "" {
		t.Fatalf("lease acquire token=%q acquired=%t err=%v", token, acquired, err)
	}
	if err := repo.ReleaseLease(ctx, "phase3-integration-lease", "test-worker", token); err != nil {
		t.Fatal(err)
	}
	if err := repo.WaitForProviderRequest(ctx, "phase3-integration-budget", 0); err != nil {
		t.Fatal(err)
	}
}

func TestPhase3PredictionGenerationIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("PHASE3_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set PHASE3_DATABASE_URL to a dedicated PostgreSQL database")
	}
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	repo := ingest.NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	historySeasonID := provider.SeasonID("91" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	targetSeasonID := provider.SeasonID("92" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	for _, season := range []provider.SeasonSummary{
		{ID: historySeasonID, TournamentID: 14149, Name: "prediction-history", StartUTC: now.Add(-3 * time.Hour), EndUTC: now.Add(-2 * time.Hour)},
		{ID: targetSeasonID, TournamentID: 14149, Name: "prediction-target", StartUTC: now.Add(-time.Hour), EndUTC: now.Add(3 * time.Hour)},
	} {
		if err := repo.UpsertSeason(ctx, season, "", now); err != nil {
			t.Fatal(err)
		}
	}
	homeHistory := provider.TeamRef{UID: provider.TeamUID(9101), ProviderID: 9101, Name: "History Home"}
	awayHistory := provider.TeamRef{UID: provider.TeamUID(9102), ProviderID: 9102, Name: "History Away"}
	for _, team := range []provider.TeamRef{homeHistory, awayHistory} {
		if err := repo.UpsertTeam(ctx, team, "", now); err != nil {
			t.Fatal(err)
		}
	}
	historyFixture := provider.Fixture{
		MatchID: provider.MatchID(910000001), ProviderMatchID: "910000001", SeasonID: historySeasonID,
		TournamentID: 14149, Round: 1, ScheduledUTS: now.Add(-2 * time.Hour).Unix(), ScheduledAt: now.Add(-2 * time.Hour),
		Home: homeHistory, Away: awayHistory, Score: &provider.Score{Home: 1, Away: 0},
	}
	if err := repo.UpsertFixture(ctx, historyFixture, "", now, 0); err != nil {
		t.Fatal(err)
	}
	targets := make([]provider.Fixture, 0, 8)
	for index := 0; index < 8; index++ {
		home := provider.TeamRef{UID: provider.TeamUID(9201 + index*2), ProviderID: int64(9201 + index*2), Name: "Target Home " + strconv.Itoa(index)}
		away := provider.TeamRef{UID: provider.TeamUID(9202 + index*2), ProviderID: int64(9202 + index*2), Name: "Target Away " + strconv.Itoa(index)}
		for _, team := range []provider.TeamRef{home, away} {
			if err := repo.UpsertTeam(ctx, team, "", now); err != nil {
				t.Fatal(err)
			}
		}
		fixture := provider.Fixture{
			MatchID: provider.MatchID(920000000 + uint64(index+1)), ProviderMatchID: strconv.Itoa(920000000 + index + 1),
			SeasonID: targetSeasonID, TournamentID: 14149, Round: 1,
			ScheduledUTS: now.Add(2*time.Hour + time.Duration(index)*time.Minute).Unix(),
			ScheduledAt:  now.Add(2*time.Hour + time.Duration(index)*time.Minute), Home: home, Away: away,
		}
		if err := repo.UpsertFixture(ctx, fixture, "", now, 0); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, fixture)
	}
	engine := NewEngine(db)
	engine.Now = func() time.Time { return now }
	first, err := engine.GenerateRound(ctx, targetSeasonID.String(), 1, DefaultConfig(), false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.GenerateRound(ctx, targetSeasonID.String(), 1, DefaultConfig(), false)
	if err != nil {
		t.Fatal(err)
	}
	if first.GeneratedCount != 8 || second.GeneratedCount != 8 || !second.ExistingRun {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var runCount, revisionCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM prediction_runs WHERE season_id = $1 AND round_no = 1 AND prediction_policy = $2`, targetSeasonID.String(), DefaultConfig().PolicyVersion).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM prediction_revisions WHERE prediction_run_id = $1::uuid`, first.RunID).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 || revisionCount != 8 {
		t.Fatalf("idempotent rows runs=%d revisions=%d", runCount, revisionCount)
	}
}
