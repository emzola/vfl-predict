package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/prediction"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	seasonID := ""
	round := 0
	modelName := "poisson"
	freeze := false
	runOnce := false
	interval := 60 * time.Second
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.StringVar(&seasonID, "season-id", seasonID, "season to predict; required for run-once")
	flag.IntVar(&round, "round", round, "round to predict; required for run-once")
	flag.StringVar(&modelName, "model", modelName, "Phase 2 baseline model name")
	flag.BoolVar(&freeze, "freeze", freeze, "create an official frozen round prediction")
	flag.BoolVar(&runOnce, "run-once", runOnce, "generate one round and exit")
	flag.DurationVar(&interval, "interval", interval, "background safety-scan interval")
	flag.Parse()
	if databaseURL == "" {
		fatal("set DATABASE_URL or -database-url")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, migrationsDir); err != nil {
		fatal(err.Error())
	}
	repo := ingest.NewRepository(db)
	engine := prediction.NewEngine(db)
	config := prediction.DefaultConfig()
	config.ModelName = modelName
	ownerID := fmt.Sprintf("prediction-worker-%d", os.Getpid())
	generate := func() {
		token, acquired, leaseErr := repo.AcquireLease(ctx, "prediction-worker", ownerID, 4*time.Minute)
		if leaseErr != nil || !acquired {
			return
		}
		defer func() { _ = repo.ReleaseLease(context.Background(), "prediction-worker", ownerID, token) }()
		targetSeason, targetRound := seasonID, round
		if targetSeason == "" || targetRound <= 0 {
			var nextErr error
			targetSeason, targetRound, nextErr = nextRound(ctx, db)
			if nextErr != nil || targetSeason == "" || targetRound <= 0 {
				return
			}
		}
		executionID, executionErr := repo.StartJobExecution(ctx, "prediction-worker", "prediction-worker", 1,
			fmt.Sprintf(`{"season_id":%q,"round":%d,"freeze":%t}`, targetSeason, targetRound, freeze))
		if executionErr != nil {
			return
		}
		report, generationErr := engine.GenerateRound(ctx, targetSeason, targetRound, config, freeze)
		if generationErr != nil {
			retryable := true
			_ = repo.FinishJobExecution(ctx, executionID, "failed", "prediction_generation_failed", generationErr.Error(), &retryable, 0, 0, `{}`)
			return
		}
		_ = repo.FinishJobExecution(ctx, executionID, "succeeded", "", "", nil, report.GeneratedCount, 0,
			fmt.Sprintf(`{"prediction_status":%q,"run_id":%q}`, report.Status, report.RunID))
	}
	if runOnce {
		if seasonID == "" || round < 1 || round > 30 {
			fatal("-season-id and -round 1..30 are required with -run-once")
		}
		token, acquired, err := repo.AcquireLease(ctx, "prediction-worker", ownerID, 4*time.Minute)
		if err != nil {
			fatal(err.Error())
		}
		if !acquired {
			fatal("prediction worker lease is held by another worker")
		}
		executionID, err := repo.StartJobExecution(ctx, "prediction-worker", "prediction-worker", 1,
			fmt.Sprintf(`{"season_id":%q,"round":%d,"freeze":%t}`, seasonID, round, freeze))
		if err != nil {
			_ = repo.ReleaseLease(context.Background(), "prediction-worker", ownerID, token)
			fatal(err.Error())
		}
		report, err := engine.GenerateRound(ctx, seasonID, round, config, freeze)
		if err != nil {
			retryable := true
			_ = repo.FinishJobExecution(ctx, executionID, "failed", "prediction_generation_failed", err.Error(), &retryable, 0, 0, `{}`)
		} else {
			_ = repo.FinishJobExecution(ctx, executionID, "succeeded", "", "", nil, report.GeneratedCount, 0,
				fmt.Sprintf(`{"prediction_status":%q,"run_id":%q}`, report.Status, report.RunID))
		}
		_ = repo.ReleaseLease(context.Background(), "prediction-worker", ownerID, token)
		if err != nil {
			fatal(err.Error())
		}
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(encoded))
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		generate()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func nextRound(ctx context.Context, db *sql.DB) (string, int, error) {
	var season string
	var round int
	row := db.QueryRowContext(ctx, `
SELECT season_id, round_no FROM round_states
WHERE state NOT IN ('settled') AND earliest_kickoff_at >= now()
ORDER BY earliest_kickoff_at LIMIT 1`)
	if err := row.Scan(&season, &round); err != nil {
		return "", 0, err
	}
	return season, round, nil
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "prediction-worker:", message)
	os.Exit(1)
}
