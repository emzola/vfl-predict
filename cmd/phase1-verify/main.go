package main

import (
	"context"
	"fmt"
	"os"

	"bet9ja-vfl/services/ingest"
)

func main() {
	databaseURL := os.Getenv("PHASE1_DATABASE_URL")
	if databaseURL == "" {
		fmt.Println("Phase 1 database integration is skipped: set PHASE1_DATABASE_URL to a dedicated PostgreSQL database.")
		return
	}
	db, err := ingest.OpenDatabase(context.Background(), databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(context.Background(), db, "db/migrations"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Phase 1 database migrations applied.")
}
