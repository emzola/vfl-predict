package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	modelName := "bookmaker_first"
	freeze := false
	autoFreeze := false
	runOnce := false
	interval := 60 * time.Second
	safetyBuffer, err := durationFromEnvSeconds("PREDICTION_SAFETY_BUFFER_SECONDS", prediction.DefaultConfig().SafetyBuffer)
	if err != nil {
		fatal(err.Error())
	}
	freezeLead, err := durationFromEnvSeconds("PREDICTION_FREEZE_LEAD_SECONDS", 90*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.StringVar(&seasonID, "season-id", seasonID, "season to predict; required for run-once")
	flag.IntVar(&round, "round", round, "round to predict; required for run-once")
	flag.StringVar(&modelName, "model", modelName, "prediction policy/model name (default: bookmaker_first)")
	flag.BoolVar(&freeze, "freeze", freeze, "create an official frozen round prediction")
	flag.BoolVar(&autoFreeze, "auto-freeze", autoFreeze, "freeze the selected upcoming round during the configured pre-kickoff window")
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
	if err := ingest.ApplyMigrationsIfEnabled(ctx, db, migrationsDir); err != nil {
		fatal(err.Error())
	}
	repo := ingest.NewRepository(db)
	engine := prediction.NewEngine(db)
	config := prediction.DefaultConfig()
	config.ModelName = modelName
	config.SafetyBuffer = safetyBuffer
	ownerID := fmt.Sprintf("prediction-worker-%d", os.Getpid())
	generate := func() {
		token, acquired, leaseErr := repo.AcquireLease(ctx, "prediction-worker", ownerID, 4*time.Minute)
		if leaseErr != nil || !acquired {
			return
		}
		defer func() { _ = repo.ReleaseLease(context.Background(), "prediction-worker", ownerID, token) }()
		target := roundTarget{SeasonID: seasonID, Round: round}
		if target.SeasonID == "" || target.Round <= 0 {
			var nextErr error
			target, nextErr = nextRound(ctx, db)
			if nextErr != nil || target.SeasonID == "" || target.Round <= 0 {
				return
			}
		}
		freezeRun := shouldFreeze(target, time.Now().UTC(), config, freeze, autoFreeze, freezeLead)
		executionID, executionErr := repo.StartJobExecution(ctx, "prediction-worker", "prediction-worker", 1,
			fmt.Sprintf(`{"season_id":%q,"round":%d,"freeze":%t}`, target.SeasonID, target.Round, freezeRun))
		if executionErr != nil {
			return
		}
		report, generationErr := engine.GenerateRound(ctx, target.SeasonID, target.Round, config, freezeRun)
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

type roundTarget struct {
	SeasonID          string
	Round             int
	EarliestKickoffAt time.Time
}

func nextRound(ctx context.Context, db *sql.DB) (roundTarget, error) {
	var target roundTarget
	row := db.QueryRowContext(ctx, `
SELECT rs.season_id, rs.round_no, rs.earliest_kickoff_at
FROM round_states rs
WHERE rs.state NOT IN ('settled', 'missed_cutoff')
  AND rs.earliest_kickoff_at >= now()
  AND NOT EXISTS (
      SELECT 1
      FROM prediction_runs official
      WHERE official.season_id = rs.season_id
        AND official.round_no = rs.round_no
        AND official.status IN ('frozen', 'settled')
        AND official.generated_fixture_count = 8
  )
ORDER BY rs.earliest_kickoff_at LIMIT 1`)
	if err := row.Scan(&target.SeasonID, &target.Round, &target.EarliestKickoffAt); err != nil {
		return roundTarget{}, err
	}
	return target, nil
}

func shouldFreeze(target roundTarget, now time.Time, config prediction.Config, force, auto bool, lead time.Duration) bool {
	if force {
		return true
	}
	if !auto || target.EarliestKickoffAt.IsZero() {
		return false
	}
	cutoff := target.EarliestKickoffAt.Add(-config.SafetyBuffer)
	return !now.Before(cutoff.Add(-lead))
}

func durationFromEnvSeconds(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of seconds", name)
	}
	return time.Duration(seconds) * time.Second, nil
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "prediction-worker:", message)
	os.Exit(1)
}
