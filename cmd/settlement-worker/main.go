package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bet9ja-vfl/services/autobet"
	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/settlement"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	interval := 5 * time.Second
	runOnce := false
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.DurationVar(&interval, "interval", interval, "settlement scan interval")
	flag.BoolVar(&runOnce, "run-once", runOnce, "settle once and exit")
	flag.Parse()
	if databaseURL == "" {
		fatal("set DATABASE_URL or -database-url")
	}
	if interval < 5*time.Second {
		fatal("settlement interval must be at least 5 seconds")
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
	ownerID := fmt.Sprintf("settlement-worker-%d", os.Getpid())
	settler := settlement.NewSettler(db)
	autoBetService := autobet.NewService(db)
	cycle := func() {
		token, acquired, leaseErr := repo.AcquireLease(ctx, "settlement-worker", ownerID, 4*time.Minute)
		if leaseErr != nil || !acquired {
			return
		}
		defer func() { _ = repo.ReleaseLease(context.Background(), "settlement-worker", ownerID, token) }()
		_, autoBetErr := autoBetService.Settle(ctx)
		report, settleErr := settler.Settle(ctx, "")
		if autoBetErr != nil || settleErr != nil {
			return
		}
		encoded, _ := json.Marshal(report)
		fmt.Println(string(encoded))
	}
	if runOnce {
		cycle()
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		cycle()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "settlement-worker:", message)
	os.Exit(1)
}
