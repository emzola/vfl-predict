package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/storageaudit"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL")
	output := flag.String("output", "", "optional JSON output path")
	flag.Parse()
	if strings.TrimSpace(*databaseURL) == "" {
		fatal("set DATABASE_URL or -database-url")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, *databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	plan, err := storageaudit.LoadRetentionPlan(ctx, db, time.Now().UTC())
	if err != nil {
		fatal(err.Error())
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fatal("encode retention plan: " + err.Error())
	}
	if *output != "" {
		if parent := outputParent(*output); parent != "." {
			if err := os.MkdirAll(parent, 0o755); err != nil {
				fatal("create retention report directory: " + err.Error())
			}
		}
		if err := os.WriteFile(*output, append(encoded, '\n'), 0o644); err != nil {
			fatal("write retention plan: " + err.Error())
		}
	}
	fmt.Println("DRY RUN ONLY: no rows were deleted or archived.")
	for _, candidate := range plan.Policies {
		retention := "manual"
		if candidate.RetentionDays != nil {
			retention = fmt.Sprintf("%d days", *candidate.RetentionDays)
		}
		fmt.Printf("%s class=%s action=%s enabled=%t retention=%s candidates=%d bytes=%s review=%t\n",
			candidate.PolicyKey, candidate.DataClass, candidate.Action, candidate.Enabled, retention,
			candidate.CandidateRows, storageaudit.FormatBytes(candidate.CandidateBytes), candidate.RequiresReview)
		fmt.Printf("  %s\n", candidate.Notes)
	}
	for _, warning := range plan.Warnings {
		fmt.Printf("warning=%s\n", warning)
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func outputParent(path string) string {
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
