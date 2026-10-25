// Package storageaudit contains read-only PostgreSQL storage inspection and
// retention dry-run helpers.  It intentionally has no deletion or archive
// implementation: Phase 4.2 records policy and evidence before any cleanup is
// approved.
package storageaudit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RelationMetric is the PostgreSQL-reported physical footprint of one user
// relation. TotalBytes includes indexes and TOAST; TableBytes is the main
// relation fork; IndexBytes is the index footprint; ToastBytes is the TOAST
// relation footprint where PostgreSQL exposes one.
type RelationMetric struct {
	Schema          string `json:"schema"`
	Name            string `json:"name"`
	TotalBytes      int64  `json:"total_bytes"`
	TableBytes      int64  `json:"table_bytes"`
	IndexBytes      int64  `json:"index_bytes"`
	ToastBytes      int64  `json:"toast_bytes"`
	EstimatedRows   int64  `json:"estimated_rows"`
	EstimatedDead   int64  `json:"estimated_dead_rows"`
	LastAutovacuum  string `json:"last_autovacuum,omitempty"`
	LastAutoanalyze string `json:"last_autoanalyze,omitempty"`
}

type PayloadMetric struct {
	Rows                int64 `json:"rows"`
	PayloadTextBytes    int64 `json:"payload_text_bytes"`
	PayloadJSONBytes    int64 `json:"payload_json_bytes"`
	RequestURLBytes     int64 `json:"request_url_bytes"`
	RequestParamsBytes  int64 `json:"request_params_bytes"`
	ProvenanceJSONBytes int64 `json:"provenance_json_bytes"`
}

type WALMetric struct {
	DirectoryBytes   int64         `json:"directory_bytes"`
	FileCount        int64         `json:"file_count"`
	CumulativeBytes  int64         `json:"cumulative_wal_bytes"`
	StatsReset       string        `json:"stats_reset,omitempty"`
	ReplicationHolds []WALSlotHold `json:"replication_holds,omitempty"`
}

type WALSlotHold struct {
	SlotName      string `json:"slot_name"`
	RetainedBytes int64  `json:"retained_bytes"`
	Active        bool   `json:"active"`
}

type DatabaseReport struct {
	CollectedAt   time.Time         `json:"collected_at"`
	DatabaseName  string            `json:"database_name"`
	DatabaseBytes int64             `json:"database_bytes"`
	Relations     []RelationMetric  `json:"relations"`
	Payloads      PayloadMetric     `json:"payloads"`
	WAL           WALMetric         `json:"wal"`
	RowCounts     map[string]int64  `json:"row_counts"`
	Settings      map[string]string `json:"settings"`
	Warnings      []string          `json:"warnings,omitempty"`
}

// CollectDatabase performs only SELECTs and PostgreSQL catalog reads.  It is
// safe to run against a production database while the application is online.
func CollectDatabase(ctx context.Context, db *sql.DB) (DatabaseReport, error) {
	if db == nil {
		return DatabaseReport{}, errors.New("storage audit database is nil")
	}
	report := DatabaseReport{
		CollectedAt: time.Now().UTC(),
		RowCounts:   make(map[string]int64),
		Settings:    make(map[string]string),
	}
	if err := db.QueryRowContext(ctx, `
		SELECT current_database(), pg_database_size(current_database())
	`).Scan(&report.DatabaseName, &report.DatabaseBytes); err != nil {
		return DatabaseReport{}, fmt.Errorf("query database size: %w", err)
	}

	relations, err := collectRelations(ctx, db)
	if err != nil {
		return DatabaseReport{}, err
	}
	report.Relations = relations

	if err := db.QueryRowContext(ctx, `
		SELECT count(*)::bigint,
		       COALESCE(sum(octet_length(payload_text)), 0)::bigint,
		       COALESCE(sum(pg_column_size(payload)), 0)::bigint,
		       COALESCE(sum(octet_length(request_url)), 0)::bigint,
		       COALESCE(sum(pg_column_size(request_params)), 0)::bigint,
		       COALESCE(sum(pg_column_size(provenance)), 0)::bigint
		FROM raw_payloads
	`).Scan(
		&report.Payloads.Rows,
		&report.Payloads.PayloadTextBytes,
		&report.Payloads.PayloadJSONBytes,
		&report.Payloads.RequestURLBytes,
		&report.Payloads.RequestParamsBytes,
		&report.Payloads.ProvenanceJSONBytes,
	); err != nil {
		report.Warnings = append(report.Warnings, "raw_payloads logical-byte query unavailable: "+err.Error())
	}

	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(sum(size), 0)::bigint, count(*)::bigint
		FROM pg_ls_waldir()
	`).Scan(&report.WAL.DirectoryBytes, &report.WAL.FileCount); err != nil {
		report.Warnings = append(report.Warnings, "pg_ls_waldir unavailable: "+err.Error())
	}
	var cumulative string
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(wal_bytes, 0)::text, COALESCE(stats_reset::text, '')
		FROM pg_stat_wal
	`).Scan(&cumulative, &report.WAL.StatsReset); err == nil {
		report.WAL.CumulativeBytes, _ = strconv.ParseInt(cumulative, 10, 64)
	} else {
		report.Warnings = append(report.Warnings, "pg_stat_wal unavailable: "+err.Error())
	}
	if err := collectReplicationSlots(ctx, db, &report.WAL); err != nil {
		report.Warnings = append(report.Warnings, "replication slot query unavailable: "+err.Error())
	}

	rows, err := db.QueryContext(ctx, `
		SELECT name, setting || CASE WHEN unit IS NULL OR unit = '' THEN '' ELSE ' ' || unit END
		FROM pg_settings
		WHERE name IN (
			'data_directory', 'max_wal_size', 'min_wal_size', 'wal_keep_size',
			'max_connections', 'shared_buffers', 'effective_cache_size',
			'autovacuum', 'autovacuum_max_workers',
			'autovacuum_vacuum_scale_factor', 'autovacuum_analyze_scale_factor'
		)
		ORDER BY name
	`)
	if err == nil {
		for rows.Next() {
			var name, value string
			if scanErr := rows.Scan(&name, &value); scanErr != nil {
				report.Warnings = append(report.Warnings, "scan PostgreSQL setting: "+scanErr.Error())
				continue
			}
			report.Settings[name] = value
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			report.Warnings = append(report.Warnings, "read PostgreSQL settings: "+rowsErr.Error())
		}
		_ = rows.Close()
	} else {
		report.Warnings = append(report.Warnings, "PostgreSQL settings query unavailable: "+err.Error())
	}

	for _, table := range auditedTables {
		count, exists, countErr := tableRowCount(ctx, db, table.Name)
		if countErr != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("count %s: %v", table.Name, countErr))
			continue
		}
		if exists {
			report.RowCounts[table.Key] = count
		}
	}
	return report, nil
}

func collectRelations(ctx context.Context, db *sql.DB) ([]RelationMetric, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT n.nspname,
		       c.relname,
		       pg_total_relation_size(c.oid)::bigint,
		       pg_relation_size(c.oid)::bigint,
		       pg_indexes_size(c.oid)::bigint,
		       CASE WHEN c.reltoastrelid <> 0
		            THEN pg_total_relation_size(c.reltoastrelid)::bigint
		            ELSE 0 END,
		       COALESCE(s.n_live_tup, 0)::bigint,
		       COALESCE(s.n_dead_tup, 0)::bigint,
		       COALESCE(s.last_autovacuum::text, ''),
		       COALESCE(s.last_autoanalyze::text, '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND c.relkind IN ('r', 'p')
		ORDER BY pg_total_relation_size(c.oid) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query PostgreSQL relation sizes: %w", err)
	}
	defer rows.Close()
	var result []RelationMetric
	for rows.Next() {
		var item RelationMetric
		if err := rows.Scan(
			&item.Schema, &item.Name, &item.TotalBytes, &item.TableBytes,
			&item.IndexBytes, &item.ToastBytes, &item.EstimatedRows,
			&item.EstimatedDead, &item.LastAutovacuum, &item.LastAutoanalyze,
		); err != nil {
			return nil, fmt.Errorf("scan PostgreSQL relation size: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PostgreSQL relation sizes: %w", err)
	}
	return result, nil
}

func collectReplicationSlots(ctx context.Context, db *sql.DB, wal *WALMetric) error {
	rows, err := db.QueryContext(ctx, `
		SELECT slot_name,
		       COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0)::bigint,
		       active
		FROM pg_replication_slots
		WHERE restart_lsn IS NOT NULL
		ORDER BY slot_name
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item WALSlotHold
		if err := rows.Scan(&item.SlotName, &item.RetainedBytes, &item.Active); err != nil {
			return err
		}
		wal.ReplicationHolds = append(wal.ReplicationHolds, item)
	}
	return rows.Err()
}

type auditedTable struct {
	Key  string
	Name string
}

var auditedTables = []auditedTable{
	{Key: "seasons", Name: "seasons"},
	{Key: "teams", Name: "teams"},
	{Key: "fixtures", Name: "fixtures"},
	{Key: "raw_payloads", Name: "raw_payloads"},
	{Key: "odds_snapshots", Name: "odds_snapshots"},
	{Key: "odds_snapshot_observations", Name: "odds_snapshot_observations"},
	{Key: "odds_change_history", Name: "odds_change_history"},
	{Key: "live_feed_observations", Name: "live_feed_observations"},
	{Key: "feature_snapshots", Name: "feature_snapshots"},
	{Key: "research_predictions", Name: "research_predictions"},
	{Key: "evaluation_metrics", Name: "evaluation_metrics"},
	{Key: "betting_analysis_selections", Name: "betting_analysis_selections"},
	{Key: "prediction_runs", Name: "prediction_runs"},
	{Key: "prediction_revisions", Name: "prediction_revisions"},
	{Key: "prediction_settlements", Name: "prediction_settlements"},
	{Key: "job_execution_history", Name: "job_execution_history"},
	{Key: "validation_errors", Name: "validation_errors"},
	{Key: "ingestion_failures", Name: "ingestion_failures"},
	{Key: "auth_audit_events", Name: "auth_audit_events"},
	{Key: "storage_audit_snapshots", Name: "storage_audit_snapshots"},
}

func tableRowCount(ctx context.Context, db *sql.DB, table string) (count int64, exists bool, err error) {
	var relation sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1)::text`, "public."+table).Scan(&relation); err != nil {
		return 0, false, err
	}
	if !relation.Valid || strings.TrimSpace(relation.String) == "" {
		return 0, false, nil
	}
	// table comes only from auditedTables above; it is not caller-controlled.
	if err := db.QueryRowContext(ctx, "SELECT count(*)::bigint FROM public."+table).Scan(&count); err != nil {
		return 0, true, err
	}
	return count, true, nil
}

type RetentionPolicy struct {
	PolicyKey     string
	DataClass     string
	RetentionDays sql.NullInt64
	Action        string
	Enabled       bool
	Scope         string
	Description   string
}

type RetentionCandidate struct {
	PolicyKey      string `json:"policy_key"`
	DataClass      string `json:"data_class"`
	Action         string `json:"action"`
	Enabled        bool   `json:"enabled"`
	RetentionDays  *int64 `json:"retention_days,omitempty"`
	CandidateRows  int64  `json:"candidate_rows"`
	CandidateBytes int64  `json:"candidate_bytes"`
	RequiresReview bool   `json:"requires_review"`
	Notes          string `json:"notes"`
}

type RetentionPlan struct {
	GeneratedAt time.Time            `json:"generated_at"`
	DryRun      bool                 `json:"dry_run"`
	Policies    []RetentionCandidate `json:"policies"`
	Warnings    []string             `json:"warnings,omitempty"`
}

// LoadRetentionPlan reads policy rows and estimates candidates. It never
// deletes, updates, archives, vacuums, or otherwise mutates data.
func LoadRetentionPlan(ctx context.Context, db *sql.DB, now time.Time) (RetentionPlan, error) {
	if db == nil {
		return RetentionPlan{}, errors.New("retention dry-run database is nil")
	}
	plan := RetentionPlan{GeneratedAt: now.UTC(), DryRun: true}
	rows, err := db.QueryContext(ctx, `
		SELECT policy_key, data_class, retention_days, action, enabled,
		       scope::text, description
		FROM storage_retention_policies
		ORDER BY policy_key
	`)
	if err != nil {
		return RetentionPlan{}, fmt.Errorf("read storage retention policies (apply migration 010 first): %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var policy RetentionPolicy
		if err := rows.Scan(&policy.PolicyKey, &policy.DataClass, &policy.RetentionDays, &policy.Action, &policy.Enabled, &policy.Scope, &policy.Description); err != nil {
			return RetentionPlan{}, fmt.Errorf("scan storage retention policy: %w", err)
		}
		candidate := RetentionCandidate{
			PolicyKey: policy.PolicyKey,
			DataClass: policy.DataClass,
			Action:    policy.Action,
			Enabled:   policy.Enabled,
			Notes:     policy.Description,
		}
		if policy.RetentionDays.Valid {
			days := policy.RetentionDays.Int64
			candidate.RetentionDays = &days
		}
		candidate.CandidateRows, candidate.CandidateBytes, candidate.RequiresReview, candidate.Notes = retentionCandidates(ctx, db, policy, now, candidate.Notes)
		plan.Policies = append(plan.Policies, candidate)
	}
	if err := rows.Err(); err != nil {
		return RetentionPlan{}, fmt.Errorf("read storage retention policies: %w", err)
	}
	return plan, nil
}

func retentionCandidates(ctx context.Context, db *sql.DB, policy RetentionPolicy, now time.Time, notes string) (int64, int64, bool, string) {
	if !policy.RetentionDays.Valid {
		return 0, 0, policy.Action != "retain", notes + " No time-based candidate calculation is defined."
	}
	cutoff := now.UTC().Add(-time.Duration(policy.RetentionDays.Int64) * 24 * time.Hour)
	cutoffArg := cutoff.Format(time.RFC3339Nano)
	query := ""
	manualReview := false
	switch policy.PolicyKey {
	case "job_execution_history":
		query = `SELECT count(*)::bigint, COALESCE(sum(pg_column_size(t)), 0)::bigint FROM public.job_execution_history t WHERE finished_at IS NOT NULL AND finished_at < $1::timestamptz`
	case "storage_audit_snapshots":
		query = `SELECT count(*)::bigint, COALESCE(sum(pg_column_size(t)), 0)::bigint FROM public.storage_audit_snapshots t WHERE collected_at < $1::timestamptz`
	case "auth_ephemeral_tokens_and_rate_limits":
		return multiTableCandidates(ctx, db, cutoffArg, notes)
	case "unverified_live_observations":
		query = `SELECT count(*)::bigint, COALESCE(sum(pg_column_size(t)), 0)::bigint FROM public.odds_snapshot_observations t WHERE point_in_time_verified = false AND observed_at < $1::timestamptz`
		manualReview = true
	case "retired_model_candidates":
		query = `SELECT count(*)::bigint, COALESCE(sum(pg_column_size(t)), 0)::bigint FROM public.model_artifacts t WHERE promotion_status = 'retired' AND created_at < $1::timestamptz`
		manualReview = true
	default:
		return 0, 0, policy.Action != "retain", notes + " No automatic candidate query is defined for this policy."
	}
	var rows, bytes int64
	if err := db.QueryRowContext(ctx, query, cutoffArg).Scan(&rows, &bytes); err != nil {
		return 0, 0, true, notes + " Candidate query unavailable: " + err.Error()
	}
	if manualReview {
		notes += " Candidates are informational only and require provenance review before any archive action."
	}
	return rows, bytes, manualReview || policy.Action != "retain", notes
}

func multiTableCandidates(ctx context.Context, db *sql.DB, cutoff string, notes string) (int64, int64, bool, string) {
	var rows, bytes int64
	for _, query := range []string{
		`SELECT count(*)::bigint, COALESCE(sum(pg_column_size(t)), 0)::bigint FROM public.auth_magic_link_tokens t WHERE expires_at < $1::timestamptz`,
		`SELECT count(*)::bigint, COALESCE(sum(pg_column_size(t)), 0)::bigint FROM public.auth_login_rate_limits t WHERE updated_at < $1::timestamptz`,
	} {
		var partRows, partBytes int64
		if err := db.QueryRowContext(ctx, query, cutoff).Scan(&partRows, &partBytes); err != nil {
			return rows, bytes, true, notes + " One or more optional auth cleanup tables are unavailable: " + err.Error()
		}
		rows += partRows
		bytes += partBytes
	}
	return rows, bytes, true, notes + " Expired operational rows are candidates only; no deletion is performed."
}

func FormatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	for _, unit := range units {
		amount /= 1024
		if amount < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.2f %s", amount, unit)
		}
	}
	return fmt.Sprintf("%d B", value)
}

type CapacityAlert struct {
	Level   string `json:"level"`
	Metric  string `json:"metric"`
	Value   int64  `json:"value"`
	Message string `json:"message"`
}

// CapacityAlerts is pure threshold logic used by the operator verification
// script. Zero total/free values mean that a filesystem measurement was not
// supplied and are not treated as an alert.
func CapacityAlerts(totalBytes, freeBytes, walBytes, dockerBuildCacheBytes int64) []CapacityAlert {
	var alerts []CapacityAlert
	if totalBytes > 0 && freeBytes >= 0 {
		freePercent := float64(freeBytes) / float64(totalBytes) * 100
		if freePercent < 15 {
			alerts = append(alerts, CapacityAlert{"critical", "filesystem_free_percent", int64(freePercent * 100), "less than 15% of the filesystem is free"})
		} else if freePercent < 25 {
			alerts = append(alerts, CapacityAlert{"warning", "filesystem_free_percent", int64(freePercent * 100), "less than 25% of the filesystem is free"})
		}
	}
	if walBytes > 2*1024*1024*1024 {
		alerts = append(alerts, CapacityAlert{"warning", "wal_bytes", walBytes, "WAL exceeds 2 GiB; inspect checkpoints, slots, and archiving"})
	}
	if dockerBuildCacheBytes > 10*1024*1024*1024 {
		alerts = append(alerts, CapacityAlert{"warning", "docker_build_cache_bytes", dockerBuildCacheBytes, "Docker build cache exceeds 10 GiB; review unused cache before pruning"})
	}
	return alerts
}
