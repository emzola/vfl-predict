package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/storageaudit"
)

type auditBundle struct {
	Database storageaudit.DatabaseReport `json:"database"`
	Docker   map[string]string           `json:"docker,omitempty"`
	ReadOnly bool                        `json:"read_only"`
}

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL")
	output := flag.String("output", "", "optional JSON output path")
	markdown := flag.String("markdown", "", "optional Markdown output path")
	includeDocker := flag.Bool("include-docker", false, "include read-only Docker system inventory")
	persist := flag.Bool("persist", false, "persist a database-level audit snapshot; requires migration 010")
	source := flag.String("source", "phase42-storage-audit", "source label for a persisted audit snapshot")
	maxWALBytes := flag.Int64("max-wal-bytes", 2*1024*1024*1024, "warning/exit threshold for WAL directory bytes; zero disables")
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
	report, err := storageaudit.CollectDatabase(ctx, db)
	if err != nil {
		fatal(err.Error())
	}
	bundle := auditBundle{Database: report, ReadOnly: true}
	if *includeDocker {
		bundle.Docker = collectDocker(ctx)
	}
	if *persist {
		if err := persistSnapshot(ctx, db, report, *source); err != nil {
			fatal("persist storage audit snapshot: " + err.Error())
		}
		fmt.Println("persisted database-level storage audit snapshot")
	}

	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fatal("encode audit report: " + err.Error())
	}
	if *output != "" {
		if err := writeFile(*output, append(encoded, '\n')); err != nil {
			fatal(err.Error())
		}
	}
	if *markdown != "" {
		if err := writeFile(*markdown, []byte(formatMarkdown(bundle))); err != nil {
			fatal(err.Error())
		}
	}
	fmt.Printf("database=%s size=%s relations=%d wal=%s\n", report.DatabaseName, storageaudit.FormatBytes(report.DatabaseBytes), len(report.Relations), storageaudit.FormatBytes(report.WAL.DirectoryBytes))
	for _, relation := range report.Relations {
		fmt.Printf("%s.%s total=%s table=%s indexes=%s toast=%s rows=%d dead=%d\n",
			relation.Schema, relation.Name,
			storageaudit.FormatBytes(relation.TotalBytes), storageaudit.FormatBytes(relation.TableBytes),
			storageaudit.FormatBytes(relation.IndexBytes), storageaudit.FormatBytes(relation.ToastBytes),
			relation.EstimatedRows, relation.EstimatedDead)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("warning=%s\n", warning)
	}
	if *maxWALBytes > 0 && report.WAL.DirectoryBytes > *maxWALBytes {
		fatal(fmt.Sprintf("WAL directory is %s, above configured threshold %s", storageaudit.FormatBytes(report.WAL.DirectoryBytes), storageaudit.FormatBytes(*maxWALBytes)))
	}
}

func persistSnapshot(ctx context.Context, db *sql.DB, report storageaudit.DatabaseReport, source string) error {
	measurements, err := json.Marshal(map[string]any{
		"relations":  report.Relations,
		"payloads":   report.Payloads,
		"row_counts": report.RowCounts,
		"settings":   report.Settings,
		"warnings":   report.Warnings,
	})
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO storage_audit_snapshots (
		    collected_at, database_name, database_bytes, wal_bytes,
		    measurements, source
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, report.CollectedAt, report.DatabaseName, report.DatabaseBytes, report.WAL.DirectoryBytes, string(measurements), source)
	return err
}

func collectDocker(ctx context.Context) map[string]string {
	result := make(map[string]string)
	for key, args := range map[string][]string{
		"system_df":  {"system", "df", "-v"},
		"info":       {"info", "--format", "ServerVersion={{.ServerVersion}} OSType={{.OSType}} Driver={{.Driver}} DockerRootDir={{.DockerRootDir}}"},
		"volumes":    {"volume", "ls"},
		"containers": {"ps", "-a", "--size", "--format", "{{.Names}} {{.Status}} {{.Size}}"},
	} {
		command := exec.CommandContext(ctx, "docker", args...)
		output, err := command.CombinedOutput()
		if err != nil {
			result[key] = "unavailable: " + strings.TrimSpace(string(output))
			continue
		}
		result[key] = strings.TrimSpace(string(output))
	}
	return result
}

func formatMarkdown(bundle auditBundle) string {
	var builder strings.Builder
	builder.WriteString("# Phase 4.2 PostgreSQL storage audit\n\n")
	builder.WriteString("This report is read-only. It does not delete, archive, vacuum, or alter application data.\n\n")
	fmt.Fprintf(&builder, "- Collected: `%s`\n- Database: `%s`\n- Database size: **%s**\n- WAL directory: **%s** (%d files)\n- Raw payload rows: **%d**; logical payload text: **%s**; logical JSONB: **%s**\n\n",
		bundle.Database.CollectedAt.UTC().Format(time.RFC3339), bundle.Database.DatabaseName,
		storageaudit.FormatBytes(bundle.Database.DatabaseBytes), storageaudit.FormatBytes(bundle.Database.WAL.DirectoryBytes), bundle.Database.WAL.FileCount,
		bundle.Database.Payloads.Rows, storageaudit.FormatBytes(bundle.Database.Payloads.PayloadTextBytes), storageaudit.FormatBytes(bundle.Database.Payloads.PayloadJSONBytes))
	builder.WriteString("## Relations\n\n| Relation | Total | Main table | Indexes | TOAST | Estimated rows | Dead rows |\n|---|---:|---:|---:|---:|---:|---:|\n")
	for _, relation := range bundle.Database.Relations {
		fmt.Fprintf(&builder, "| `%s.%s` | %s | %s | %s | %s | %d | %d |\n", relation.Schema, relation.Name,
			storageaudit.FormatBytes(relation.TotalBytes), storageaudit.FormatBytes(relation.TableBytes),
			storageaudit.FormatBytes(relation.IndexBytes), storageaudit.FormatBytes(relation.ToastBytes), relation.EstimatedRows, relation.EstimatedDead)
	}
	builder.WriteString("\n## Row counts\n\n")
	keys := make([]string, 0, len(bundle.Database.RowCounts))
	for key := range bundle.Database.RowCounts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&builder, "- `%s`: %d\n", key, bundle.Database.RowCounts[key])
	}
	if len(bundle.Database.Warnings) > 0 {
		builder.WriteString("\n## Warnings\n\n")
		for _, warning := range bundle.Database.Warnings {
			fmt.Fprintf(&builder, "- %s\n", warning)
		}
	}
	if len(bundle.Docker) > 0 {
		builder.WriteString("\n## Docker inventory\n\n")
		for _, key := range []string{"info", "system_df", "volumes", "containers"} {
			if value := bundle.Docker[key]; value != "" {
				fmt.Fprintf(&builder, "### %s\n\n```text\n%s\n```\n\n", key, value)
			}
		}
	}
	return builder.String()
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(parentDir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}

func parentDir(path string) string {
	if index := strings.LastIndexAny(path, `/\\`); index >= 0 {
		if index == 0 {
			return path[:1]
		}
		return path[:index]
	}
	return "."
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
