package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/settlement"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	runID := ""
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.StringVar(&runID, "run-id", runID, "optional frozen prediction run ID")
	flag.Parse()
	if databaseURL == "" {
		fatal("set DATABASE_URL or -database-url")
	}
	ctx := context.Background()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	if err := ingest.ApplyMigrationsIfEnabled(ctx, db, migrationsDir); err != nil {
		fatal(err.Error())
	}
	report, err := settlement.NewSettler(db).Settle(ctx, runID)
	if err != nil {
		fatal(err.Error())
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "forward-settle:", message)
	os.Exit(1)
}
