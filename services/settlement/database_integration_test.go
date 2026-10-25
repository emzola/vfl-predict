package settlement

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

func TestPhase3ForwardSettlementIntegration(t *testing.T) {
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
	seasonID := provider.SeasonID("94" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	if err := repo.UpsertSeason(ctx, provider.SeasonSummary{ID: seasonID, TournamentID: 14149, Name: "settlement-integration", StartUTC: now.Add(-time.Hour), EndUTC: now.Add(time.Hour)}, "", now); err != nil {
		t.Fatal(err)
	}
	home := provider.TeamRef{UID: provider.TeamUID(9401), ProviderID: 9401, Name: "Settlement Home"}
	away := provider.TeamRef{UID: provider.TeamUID(9402), ProviderID: 9402, Name: "Settlement Away"}
	if err := repo.UpsertTeam(ctx, home, "", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTeam(ctx, away, "", now); err != nil {
		t.Fatal(err)
	}
	matchID := provider.MatchID(940000000 + uint64(now.UnixNano()%1_000_000))
	fixture := provider.Fixture{MatchID: matchID, ProviderMatchID: matchID.String(), SeasonID: seasonID, TournamentID: 14149, Round: 1, ScheduledUTS: now.Add(-time.Hour).Unix(), ScheduledAt: now.Add(-time.Hour), Home: home, Away: away, Score: &provider.Score{Home: 2, Away: 0}}
	if err := repo.UpsertFixture(ctx, fixture, "", now, 0); err != nil {
		t.Fatal(err)
	}
	modelID, err := research.NewResearchStorage(db).EnsureModelDefinition(ctx, "settlement_integration_model", "test.v1", "integration", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO prediction_runs (season_id, round_no, model_definition_id, model_version, run_type, status, prediction_as_of, input_cutoff_at, earliest_kickoff_at, input_dataset_version, prediction_policy, idempotency_key, generated_fixture_count, frozen_at)
VALUES ($1, 1, $2, 'test.v1', 'frozen', 'frozen', $3, $3, $4, 'settlement', 'test', $5, 8, $3)
RETURNING prediction_run_id::text`, seasonID.String(), modelID, now.Add(-2*time.Hour), now.Add(-time.Hour), "settlement-run:"+seasonID.String()).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	var revisionID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO prediction_revisions (prediction_run_id, season_id, match_id, round_no, revision_no, status, prediction_as_of, cutoff_at, home_probability, draw_probability, away_probability, eligible, frozen_at)
VALUES ($1::uuid, $2, $3, 1, 1, 'frozen', $4, $5, 0.7, 0.2, 0.1, true, $4)
RETURNING prediction_revision_id::text`, runID, seasonID.String(), matchID, now.Add(-2*time.Hour), now.Add(-time.Hour)).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	_ = revisionID
	report, err := NewSettler(db).Settle(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if report.SettledCount != 1 || report.Accuracy != 1 {
		t.Fatalf("settlement report=%+v", report)
	}
	var metricCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM forward_paper_results WHERE prediction_run_id = $1::uuid`, runID).Scan(&metricCount); err != nil {
		t.Fatal(err)
	}
	if metricCount < 3 {
		t.Fatalf("forward metric count=%d", metricCount)
	}
	corrected := fixture
	corrected.Score = &provider.Score{Home: 0, Away: 1}
	if err := repo.UpsertFixture(ctx, corrected, "", now.Add(time.Minute), 0); err != nil {
		t.Fatal(err)
	}
	correction, err := NewSettler(db).Settle(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if correction.CorrectionCount != 1 || correction.SettledCount != 1 || correction.Accuracy != 0 {
		t.Fatalf("correction report=%+v", correction)
	}
	var settlementCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM prediction_settlements ps JOIN prediction_revisions pr ON pr.prediction_revision_id = ps.prediction_revision_id WHERE pr.prediction_run_id = $1::uuid`, runID).Scan(&settlementCount); err != nil {
		t.Fatal(err)
	}
	if settlementCount != 2 {
		t.Fatalf("settlement correction rows=%d want 2", settlementCount)
	}
}
