package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/prediction"
	"bet9ja-vfl/services/research"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	seasonID := ""
	round := 0
	modelName := strings.TrimSpace(os.Getenv("PREDICTION_MODEL"))
	if modelName == "" {
		modelName = "bookmaker_first"
	}
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
	generationTimeout, err := durationFromEnvSeconds("PREDICTION_GENERATION_TIMEOUT_SECONDS", 90*time.Second)
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
	if strings.EqualFold(modelName, research.DynamicBayesianModelName) {
		config.PolicyVersion = "phase6.pre_match.dynamic_bayesian.v1"
	}
	config.SafetyBuffer = safetyBuffer
	ownerID := fmt.Sprintf("prediction-worker-%d", os.Getpid())
	log.Printf("prediction worker started model=%s interval=%s generation_timeout=%s", config.ModelName, interval, generationTimeout)
	generate := func() {
		token, acquired, leaseErr := repo.AcquireLease(ctx, "prediction-worker", ownerID, 4*time.Minute)
		if leaseErr != nil {
			log.Printf("lease acquisition failed: %v", leaseErr)
			return
		}
		if !acquired {
			return
		}
		defer func() { _ = repo.ReleaseLease(context.Background(), "prediction-worker", ownerID, token) }()
		target := roundTarget{SeasonID: seasonID, Round: round}
		if target.SeasonID == "" || target.Round <= 0 {
			var nextErr error
			target, nextErr = nextRound(ctx, db)
			if nextErr != nil {
				if nextErr != sql.ErrNoRows {
					log.Printf("next prediction round lookup failed: %v", nextErr)
				}
				return
			}
			if target.SeasonID == "" || target.Round <= 0 {
				return
			}
		}
		freezeRun := shouldFreeze(target, time.Now().UTC(), config, freeze, autoFreeze, freezeLead)
		executionID, executionErr := repo.StartJobExecution(ctx, "prediction-worker", "prediction-worker", 1,
			fmt.Sprintf(`{"season_id":%q,"round":%d,"freeze":%t}`, target.SeasonID, target.Round, freezeRun))
		if executionErr != nil {
			log.Printf("could not start prediction job for season=%s round=%d: %v", target.SeasonID, target.Round, executionErr)
			return
		}
		log.Printf("prediction generation started season=%s round=%d model=%s freeze=%t", target.SeasonID, target.Round, config.ModelName, freezeRun)
		generationCtx, cancelGeneration := context.WithTimeout(ctx, generationTimeout)
		report, generationErr := engine.GenerateRound(generationCtx, target.SeasonID, target.Round, config, freezeRun)
		cancelGeneration()
		if generationErr != nil {
			retryable := true
			details := fmt.Sprintf(`{"season_id":%q,"round":%d,"model":%q}`, target.SeasonID, target.Round, config.ModelName)
			_ = repo.FinishJobExecution(ctx, executionID, "failed", "prediction_generation_failed", generationErr.Error(), &retryable, 0, 0, details)
			log.Printf("prediction generation failed season=%s round=%d model=%s: %v", target.SeasonID, target.Round, config.ModelName, generationErr)
			return
		}
		reportJSON, _ := json.Marshal(report)
		_ = repo.FinishJobExecution(ctx, executionID, "succeeded", "", "", nil, report.GeneratedCount, 0,
			string(reportJSON))
		if report.Status != prediction.StatusProvisional && report.Status != prediction.StatusFrozen && report.Status != prediction.StatusSettled {
			log.Printf("prediction round did not produce an eligible run season=%s round=%d status=%s fixtures=%d generated=%d warnings=%v", report.SeasonID, report.Round, report.Status, report.FixtureCount, report.GeneratedCount, report.Warnings)
		} else {
			log.Printf("prediction round generated season=%s round=%d model=%s status=%s fixtures=%d generated=%d", report.SeasonID, report.Round, config.ModelName, report.Status, report.FixtureCount, report.GeneratedCount)
		}
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
		generationCtx, cancelGeneration := context.WithTimeout(ctx, generationTimeout)
		report, err := engine.GenerateRound(generationCtx, seasonID, round, config, freeze)
		cancelGeneration()
		if err != nil {
			retryable := true
			details := fmt.Sprintf(`{"season_id":%q,"round":%d,"model":%q}`, seasonID, round, config.ModelName)
			_ = repo.FinishJobExecution(ctx, executionID, "failed", "prediction_generation_failed", err.Error(), &retryable, 0, 0, details)
			log.Printf("prediction generation failed season=%s round=%d model=%s: %v", seasonID, round, config.ModelName, err)
		} else {
			reportJSON, _ := json.Marshal(report)
			_ = repo.FinishJobExecution(ctx, executionID, "succeeded", "", "", nil, report.GeneratedCount, 0,
				string(reportJSON))
			log.Printf("prediction run-once completed season=%s round=%d model=%s status=%s fixtures=%d generated=%d warnings=%v", report.SeasonID, report.Round, config.ModelName, report.Status, report.FixtureCount, report.GeneratedCount, report.Warnings)
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
