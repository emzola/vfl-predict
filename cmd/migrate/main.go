package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"bet9ja-vfl/services/ingest"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
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
	if err := ingest.ApplyMigrations(ctx, db, migrationsDir); err != nil {
		fatal(err.Error())
	}
	fmt.Println("migrations applied and verified")
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "migrate:", message)
	os.Exit(1)
}
