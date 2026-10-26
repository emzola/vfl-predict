package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/prediction"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "SQL migrations directory")
	seasonID := flag.String("season-id", "", "season to shadow")
	round := flag.Int("round", 0, "round 1..30 to shadow")
	safetyBuffer := flag.Duration("safety-buffer", 30*time.Second, "cutoff safety buffer")
	flag.Parse()
	if *databaseURL == "" || *seasonID == "" || *round < 1 || *round > 30 {
		if *databaseURL == "" {
			fatal("set DATABASE_URL or -database-url")
		}
		if *seasonID != "" || *round != 0 {
			fatal("provide both -season-id and -round, or omit both to select the next eligible round")
		}
	}
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, *databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, *migrationsDir); err != nil {
		fatal(err.Error())
	}
	if *seasonID == "" && *round == 0 {
		if err := db.QueryRowContext(ctx, `
SELECT season_id, round_no
FROM round_states
WHERE state NOT IN ('settled', 'missed_cutoff')
  AND earliest_kickoff_at >= now()
ORDER BY earliest_kickoff_at
LIMIT 1`).Scan(seasonID, round); err != nil {
			fatal(fmt.Sprintf("select next eligible shadow round: %v", err))
		}
	}
	config := prediction.DefaultConfig()
	config.ModelName = "dynamic_bayesian_team_strength"
	config.PolicyVersion = "phase6.shadow.v1"
	config.SafetyBuffer = *safetyBuffer
	report, err := prediction.NewEngine(db).GenerateShadowRound(ctx, *seasonID, *round, config)
	if err != nil {
		fatal(err.Error())
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
	if report.Status != "generated" {
		os.Exit(1)
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "phase6-shadow:", message)
	os.Exit(1)
}
