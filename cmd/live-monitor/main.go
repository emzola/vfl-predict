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

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/live"
)

func main() {
	cfg := ingest.ConfigFromEnv()
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	allowNetwork := false
	runOnce := false
	spacingSeconds := cfg.RequestSpacing.Seconds()
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.BoolVar(&allowNetwork, "allow-network", allowNetwork, "enable authorized read-only provider requests")
	flag.BoolVar(&runOnce, "run-once", runOnce, "execute one monitor cycle and exit")
	flag.Float64Var(&spacingSeconds, "request-spacing-seconds", spacingSeconds, "minimum shared spacing between provider requests")
	flag.Parse()
	if databaseURL == "" {
		fatal("set DATABASE_URL or -database-url")
	}
	cfg.ConnectivityEnabled = allowNetwork || cfg.ConnectivityEnabled
	cfg.RequestSpacing = time.Duration(spacingSeconds * float64(time.Second))
	if cfg.ConnectivityEnabled && cfg.RequestSpacing < 10*time.Second {
		fatal("live monitoring requires at least 10 seconds between upstream requests")
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
	client := ingest.NewProviderClient(cfg, nil)
	resultClient := ingest.NewProviderClient(cfg, nil)
	if cfg.ConnectivityEnabled {
		scheduler := ingest.NewPriorityProviderRequestScheduler(repo, "bet9ja-global", cfg.RequestSpacing)
		scheduler.Attach(client, false)
		scheduler.Attach(resultClient, true)
	}
	pollConfig := live.DefaultPollConfig()
	pollConfig.OwnerID = fmt.Sprintf("live-monitor-%d", os.Getpid())
	monitor := live.NewMonitorWithProviderClients(pollConfig, cfg, client, resultClient, repo)
	if runOnce {
		if !cfg.ConnectivityEnabled {
			fatal("provider connectivity is disabled; use -allow-network for an authorized read-only run")
		}
		report, err := monitor.PollOnce(ctx)
		if err != nil {
			fatal(err.Error())
		}
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(encoded))
		return
	}
	if !cfg.ConnectivityEnabled {
		fatal("provider connectivity is disabled; use -allow-network for the background live worker")
	}
	if err := monitor.Run(ctx); err != nil && ctx.Err() == nil {
		fatal(err.Error())
	}
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "live-monitor:", message)
	os.Exit(1)
}
