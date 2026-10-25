package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bet9ja-vfl/services/ingest"
)

func main() {
	cfg := ingest.ConfigFromEnv()
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	seasonID := ""
	maxSeasons := cfg.MaxSeasons
	allowNetwork := false
	dryRun := false
	reportPath := ""
	spacingSeconds := cfg.RequestSpacing.Seconds()
	timeoutSeconds := int(cfg.RequestTimeout / time.Second)
	maxRetries := cfg.MaxRetries
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL (prefer DATABASE_URL)")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.StringVar(&seasonID, "season-id", seasonID, "one completed provider season ID; empty selects the newest bounded batch")
	flag.IntVar(&maxSeasons, "max-seasons", maxSeasons, "maximum completed seasons to process; defaults to 1 and is capped at 30")
	flag.BoolVar(&allowNetwork, "allow-network", allowNetwork, "enable authorized read-only provider requests for this run")
	flag.BoolVar(&dryRun, "dry-run", dryRun, "discover and select bounded seasons without importing fixtures or odds")
	flag.StringVar(&reportPath, "report", reportPath, "optional path for a JSON coverage report")
	flag.Float64Var(&spacingSeconds, "request-spacing-seconds", spacingSeconds, "minimum shared spacing between provider requests")
	flag.IntVar(&timeoutSeconds, "request-timeout-seconds", timeoutSeconds, "provider request timeout")
	flag.IntVar(&maxRetries, "max-retries", maxRetries, "bounded retry count for 429/5xx/network errors")
	flag.Parse()

	if databaseURL == "" {
		fatal("database URL is required; set DATABASE_URL or -database-url")
	}
	if spacingSeconds < 0 || timeoutSeconds <= 0 || maxRetries < 0 {
		fatal("request spacing must be non-negative, timeout must be positive, and retries must be non-negative")
	}
	cfg.RequestSpacing = time.Duration(spacingSeconds * float64(time.Second))
	cfg.RequestTimeout = time.Duration(timeoutSeconds) * time.Second
	cfg.MaxRetries = maxRetries
	cfg.MaxSeasons = maxSeasons
	cfg.ConnectivityEnabled = allowNetwork || cfg.ConnectivityEnabled
	if cfg.ConnectivityEnabled && cfg.RequestSpacing < 10*time.Second {
		fatal("network ingestion requires at least 10 seconds between upstream requests")
	}

	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, migrationsDir); err != nil {
		fatal(err.Error())
	}
	repo := ingest.NewRepository(db)
	client := ingest.NewProviderClient(cfg, nil)
	if cfg.ConnectivityEnabled {
		ingest.AttachDatabaseRequestGate(client, repo, cfg.RequestSpacing)
	}
	runner := ingest.NewRunner(cfg, client, repo)
	reports, err := runner.Run(ctx, ingest.BackfillOptions{SeasonID: seasonID, MaxSeasons: maxSeasons, DryRun: dryRun})
	if err != nil {
		fatal(err.Error())
	}
	encoded, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if reportPath != "" {
		if directory := filepath.Dir(reportPath); directory != "." {
			if err := os.MkdirAll(directory, 0700); err != nil {
				fatal(fmt.Sprintf("create report directory: %v", err))
			}
		}
		if err := os.WriteFile(reportPath, encoded, 0600); err != nil {
			fatal(fmt.Sprintf("write report: %v", err))
		}
	}
	fmt.Println(string(encoded))
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "historical-ingest:", message)
	os.Exit(1)
}
