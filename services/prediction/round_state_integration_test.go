package prediction

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/provider"
)

func TestPhase36PredictionRoundStateDoesNotDowngradeFrozen(t *testing.T) {
	databaseURL := os.Getenv("PHASE36_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PHASE3_DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set PHASE36_DATABASE_URL or PHASE3_DATABASE_URL to a dedicated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	seasonID := provider.SeasonID("97" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	if err := ingest.NewRepository(db).UpsertSeason(ctx, provider.SeasonSummary{ID: seasonID, TournamentID: 14149, Name: "phase36-prediction-state", StartUTC: now, EndUTC: now.Add(time.Hour)}, "", now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM round_states WHERE season_id = $1`, seasonID.String())
		_, _ = db.ExecContext(context.Background(), `DELETE FROM seasons WHERE season_id = $1`, seasonID.String())
	})
	engine := NewEngine(db)
	kickoff := now.Add(time.Hour)
	cutoff := kickoff.Add(-30 * time.Second)
	if err := engine.upsertRoundState(ctx, seasonID.String(), 1, kickoff, cutoff, StatusFrozen); err != nil {
		t.Fatal(err)
	}
	if err := engine.upsertRoundState(ctx, seasonID.String(), 1, kickoff, cutoff, StatusMissedCutoff); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state FROM round_states WHERE season_id = $1 AND round_no = 1`, seasonID.String()).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "frozen" {
		t.Fatalf("prediction engine downgraded frozen round state to %q", state)
	}
}
