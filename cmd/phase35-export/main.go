package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/research"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL")
	output := flag.String("output", "tmp/phase35-dataset.json", "output dataset JSON path")
	allowRetrospective := flag.Bool("allow-retrospective-odds", false, "include post-settlement odds as explicitly retrospective features")
	migrationsDir := flag.String("migrations-dir", "db/migrations", "migration directory")
	flag.Parse()
	if *databaseURL == "" {
		fatal("database URL is required via -database-url or DATABASE_URL")
	}
	if err := os.MkdirAll(filepathDir(*output), 0o755); err != nil {
		fatal(err.Error())
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
	dataset, err := research.ExportPhase35Dataset(ctx, db, *allowRetrospective)
	if err != nil {
		fatal(err.Error())
	}
	if err := research.WritePhase35Dataset(*output, dataset); err != nil {
		fatal(err.Error())
	}
	withOdds := 0
	for _, row := range dataset.Rows {
		if row.Bookmaker != nil {
			withOdds++
		}
	}
	fmt.Printf("phase35_dataset=%s seasons=%d rows=%d rows_with_bookmaker=%d excluded_seasons=%d fingerprint=%s odds_policy=%s\n", *output, len(dataset.Seasons), len(dataset.Rows), withOdds, len(dataset.ExcludedSeasonIDs), dataset.DatasetFingerprint, dataset.OddsPolicy)
}

func filepathDir(path string) string {
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] == '/' || path[index] == '\\' {
			if index == 0 {
				return path[:1]
			}
			return path[:index]
		}
	}
	return "."
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
