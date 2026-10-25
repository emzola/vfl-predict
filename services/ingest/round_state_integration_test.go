package ingest

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"bet9ja-vfl/services/provider"
)

func TestPhase36RoundStateDoesNotDowngradeFrozenOrSettled(t *testing.T) {
	databaseURL := os.Getenv("PHASE36_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PHASE3_DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set PHASE36_DATABASE_URL or PHASE3_DATABASE_URL to a dedicated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seasonID := provider.SeasonID("96" + strconv.FormatInt(now.UnixNano()%1_000_000_000, 10))
	if err := repo.UpsertSeason(ctx, provider.SeasonSummary{ID: seasonID, TournamentID: 14149, Name: "phase36-round-state", StartUTC: now, EndUTC: now.Add(time.Hour)}, "", now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM round_states WHERE season_id = $1`, seasonID.String())
		_, _ = db.ExecContext(context.Background(), `DELETE FROM seasons WHERE season_id = $1`, seasonID.String())
	})
	kickoff := now.Add(time.Hour)
	cutoff := kickoff.Add(-30 * time.Second)
	if err := repo.UpsertRoundState(ctx, seasonID.String(), 1, kickoff, cutoff, "frozen", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertRoundState(ctx, seasonID.String(), 1, kickoff, cutoff, "missed_cutoff", `{}`); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state FROM round_states WHERE season_id = $1 AND round_no = 1`, seasonID.String()).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "frozen" {
		t.Fatalf("frozen round state downgraded to %q", state)
	}
	if err := repo.UpsertRoundState(ctx, seasonID.String(), 1, kickoff, cutoff, "settled", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertRoundState(ctx, seasonID.String(), 1, kickoff, cutoff, "missed_cutoff", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT state FROM round_states WHERE season_id = $1 AND round_no = 1`, seasonID.String()).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "settled" {
		t.Fatalf("settled round state downgraded to %q", state)
	}
}
