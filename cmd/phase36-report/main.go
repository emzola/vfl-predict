package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bet9ja-vfl/services/ingest"
)

type phase36Report struct {
	Phase               string             `json:"phase"`
	GeneratedAt         time.Time          `json:"generated_at"`
	Lease               ingest.LeaseStatus `json:"lease"`
	Live                liveSummary        `json:"live"`
	Odds                oddsSummary        `json:"odds"`
	Rounds              []roundEvidence    `json:"rounds"`
	Settlements         settlementSummary  `json:"settlements"`
	Jobs                []jobSummary       `json:"jobs"`
	ProviderRequestNote string             `json:"provider_request_note"`
}

type liveSummary struct {
	ObservationCount       int               `json:"observation_count"`
	AcceptedCount          int               `json:"accepted_count"`
	InvalidCount           int               `json:"invalid_count"`
	UnavailableCount       int               `json:"unavailable_count"`
	CacheReusedCount       int               `json:"cache_reused_count"`
	UniqueSeasons          int               `json:"unique_seasons"`
	FirstObservedAt        *time.Time        `json:"first_observed_at,omitempty"`
	LastObservedAt         *time.Time        `json:"last_observed_at,omitempty"`
	ProviderErrorCount     int               `json:"provider_error_count"`
	PersistedResponseCount int               `json:"persisted_response_count"`
	EndpointCounts         []endpointSummary `json:"endpoint_counts"`
}

type endpointSummary struct {
	EndpointFamily string     `json:"endpoint_family"`
	Count          int        `json:"count"`
	LastObservedAt *time.Time `json:"last_observed_at,omitempty"`
}

type oddsSummary struct {
	LiveObservationCount int `json:"live_observation_count"`
	VerifiedCount        int `json:"point_in_time_verified_count"`
	UnverifiedCount      int `json:"point_in_time_unverified_count"`
	OddsChangeCount      int `json:"odds_change_count"`
}

type roundEvidence struct {
	SeasonID            string     `json:"season_id"`
	Round               int        `json:"round"`
	ProviderBacked      bool       `json:"provider_backed"`
	EarliestKickoff     time.Time  `json:"earliest_kickoff"`
	BettingCutoff       *time.Time `json:"betting_cutoff,omitempty"`
	RoundState          string     `json:"round_state"`
	LastFixtureObserved *time.Time `json:"last_fixture_observed_at,omitempty"`
	LastOddsObserved    *time.Time `json:"last_odds_observed_at,omitempty"`
	RunID               *string    `json:"prediction_run_id,omitempty"`
	ModelVersion        *string    `json:"model_version,omitempty"`
	PredictionStatus    *string    `json:"prediction_status,omitempty"`
	PredictionAsOf      *time.Time `json:"prediction_as_of,omitempty"`
	FrozenAt            *time.Time `json:"frozen_at,omitempty"`
	GeneratedFixtures   int        `json:"generated_fixture_count"`
	RevisionCount       int        `json:"revision_count"`
	FrozenRevisionCount int        `json:"frozen_revision_count"`
	SettlementCount     int        `json:"settlement_count"`
	CompletePreCutoff   bool       `json:"complete_pre_cutoff"`
	CutoffViolation     bool       `json:"cutoff_violation"`
}

type settlementSummary struct {
	RecordedForwardSettlementCount int      `json:"recorded_forward_settlement_count"`
	ProviderBackedSettlementCount  int      `json:"provider_backed_settlement_count"`
	UnclassifiedOrSyntheticCount   int      `json:"unclassified_or_synthetic_count"`
	AverageLogLoss                 *float64 `json:"average_log_loss,omitempty"`
	AverageBrierScore              *float64 `json:"average_brier_score,omitempty"`
	Accuracy                       *float64 `json:"accuracy,omitempty"`
	EligibleOddsCount              int      `json:"eligible_odds_count"`
	HypotheticalStake              *float64 `json:"hypothetical_stake,omitempty"`
	HypotheticalReturn             *float64 `json:"hypothetical_return,omitempty"`
	HypotheticalProfit             *float64 `json:"hypothetical_profit,omitempty"`
	ForwardMetricRows              int      `json:"forward_metric_rows"`
	RetrospectiveRows              int      `json:"retrospective_settlement_rows"`
}

type jobSummary struct {
	JobKey       string     `json:"job_key"`
	Status       string     `json:"status"`
	Count        int        `json:"count"`
	LastStarted  *time.Time `json:"last_started_at,omitempty"`
	LastFinished *time.Time `json:"last_finished_at,omitempty"`
}

func main() {
	databaseURL := os.Getenv("PHASE36_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	reportPath := ""
	leaseKey := "live-monitor"
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&reportPath, "report", reportPath, "optional JSON report output path")
	flag.StringVar(&leaseKey, "lease-key", leaseKey, "scheduler lease key to inspect")
	flag.Parse()
	if databaseURL == "" {
		fatal("set PHASE36_DATABASE_URL, DATABASE_URL, or -database-url")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	repo := ingest.NewRepository(db)
	lease, err := repo.GetLease(ctx, leaseKey)
	if err != nil {
		fatal(err.Error())
	}
	report := phase36Report{
		Phase:               "3.6",
		GeneratedAt:         time.Now().UTC(),
		Lease:               lease,
		ProviderRequestNote: "Counts are persisted provider responses/observations, not a claim of every upstream wire attempt; retry and cache semantics remain in job history and raw provenance.",
	}
	if err := loadLiveSummary(ctx, db, &report.Live); err != nil {
		fatal(err.Error())
	}
	if err := loadOddsSummary(ctx, db, &report.Odds); err != nil {
		fatal(err.Error())
	}
	if report.Rounds, err = loadRounds(ctx, db); err != nil {
		fatal(err.Error())
	}
	if err := loadSettlements(ctx, db, &report.Settlements); err != nil {
		fatal(err.Error())
	}
	if report.Jobs, err = loadJobs(ctx, db); err != nil {
		fatal(err.Error())
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if reportPath != "" {
		if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
			fatal(err.Error())
		}
		if err := os.WriteFile(reportPath, append(encoded, '\n'), 0o600); err != nil {
			fatal(err.Error())
		}
	}
	fmt.Println(string(encoded))
}

func loadLiveSummary(ctx context.Context, db *sql.DB, result *liveSummary) error {
	var first, last sql.NullTime
	if err := db.QueryRowContext(ctx, `
	SELECT count(*),
       count(*) FILTER (WHERE parse_status = 'accepted'),
       count(*) FILTER (WHERE parse_status = 'invalid'),
       count(*) FILTER (WHERE parse_status = 'unavailable'),
       count(*) FILTER (WHERE cache_reused),
       count(DISTINCT season_id),
       min(observed_at), max(observed_at),
       count(*) FILTER (WHERE response_status >= 400)
FROM live_feed_observations
WHERE endpoint_family LIKE 'live_%'`).Scan(
		&result.ObservationCount, &result.AcceptedCount, &result.InvalidCount,
		&result.UnavailableCount, &result.CacheReusedCount, &result.UniqueSeasons,
		&first, &last, &result.ProviderErrorCount,
	); err != nil {
		return fmt.Errorf("load live observation summary: %w", err)
	}
	result.FirstObservedAt = nullTimePointer(first)
	result.LastObservedAt = nullTimePointer(last)
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM raw_payloads WHERE endpoint_family LIKE 'live_%'`).Scan(&result.PersistedResponseCount); err != nil {
		return fmt.Errorf("load live response count: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
SELECT endpoint_family, count(*), max(observed_at)
FROM live_feed_observations
WHERE endpoint_family LIKE 'live_%'
GROUP BY endpoint_family
ORDER BY endpoint_family`)
	if err != nil {
		return fmt.Errorf("load live endpoint summary: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item endpointSummary
		var observed sql.NullTime
		if err := rows.Scan(&item.EndpointFamily, &item.Count, &observed); err != nil {
			return fmt.Errorf("scan live endpoint summary: %w", err)
		}
		item.LastObservedAt = nullTimePointer(observed)
		result.EndpointCounts = append(result.EndpointCounts, item)
	}
	return rows.Err()
}

func loadOddsSummary(ctx context.Context, db *sql.DB, result *oddsSummary) error {
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE observation_kind = 'live_observation'),
       count(*) FILTER (WHERE observation_kind = 'live_observation' AND point_in_time_verified),
       count(*) FILTER (WHERE observation_kind = 'live_observation' AND NOT point_in_time_verified)
FROM odds_snapshot_observations o
JOIN raw_payloads p ON p.raw_payload_id = o.raw_payload_id
WHERE p.endpoint_family LIKE 'live_%'`).Scan(&result.LiveObservationCount, &result.VerifiedCount, &result.UnverifiedCount); err != nil {
		return fmt.Errorf("load odds observation summary: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM odds_change_history h JOIN raw_payloads p ON p.raw_payload_id = h.raw_payload_id WHERE p.endpoint_family LIKE 'live_%'`).Scan(&result.OddsChangeCount); err != nil {
		return fmt.Errorf("load odds change count: %w", err)
	}
	return nil
}

func loadRounds(ctx context.Context, db *sql.DB) ([]roundEvidence, error) {
	rows, err := db.QueryContext(ctx, `
SELECT rs.season_id, rs.round_no,
       EXISTS (SELECT 1 FROM live_feed_observations l WHERE l.endpoint_family LIKE 'live_%' AND l.season_id = rs.season_id AND (l.round_no IS NULL OR l.round_no = rs.round_no)) AS provider_backed,
       rs.earliest_kickoff_at, rs.betting_cutoff_at,
       rs.state, rs.last_fixture_observed_at, rs.last_odds_observed_at,
       pr.prediction_run_id::text, pr.model_version, pr.status,
       pr.prediction_as_of, pr.frozen_at, COALESCE(pr.generated_fixture_count, 0),
       COALESCE((SELECT count(*) FROM prediction_revisions r WHERE r.prediction_run_id = pr.prediction_run_id), 0),
       COALESCE((SELECT count(*) FROM prediction_revisions r WHERE r.prediction_run_id = pr.prediction_run_id AND r.status = 'frozen'), 0),
       COALESCE((SELECT count(*) FROM prediction_settlements s JOIN prediction_revisions r ON r.prediction_revision_id = s.prediction_revision_id WHERE r.prediction_run_id = pr.prediction_run_id), 0)
FROM round_states rs
LEFT JOIN LATERAL (
    SELECT p.*
    FROM prediction_runs p
    WHERE p.season_id = rs.season_id AND p.round_no = rs.round_no
    ORDER BY CASE WHEN p.status = 'frozen' THEN 0 WHEN p.run_type = 'frozen' THEN 1 ELSE 2 END,
             p.prediction_as_of DESC
    LIMIT 1
) pr ON true
ORDER BY rs.earliest_kickoff_at, rs.season_id, rs.round_no`)
	if err != nil {
		return nil, fmt.Errorf("load round evidence: %w", err)
	}
	defer rows.Close()
	result := make([]roundEvidence, 0)
	for rows.Next() {
		var item roundEvidence
		var cutoff, fixtureObserved, oddsObserved sql.NullTime
		var runID, modelVersion, predictionStatus sql.NullString
		var predictionAsOf, frozenAt sql.NullTime
		if err := rows.Scan(&item.SeasonID, &item.Round, &item.ProviderBacked, &item.EarliestKickoff, &cutoff,
			&item.RoundState, &fixtureObserved, &oddsObserved, &runID, &modelVersion,
			&predictionStatus, &predictionAsOf, &frozenAt, &item.GeneratedFixtures,
			&item.RevisionCount, &item.FrozenRevisionCount, &item.SettlementCount); err != nil {
			return nil, fmt.Errorf("scan round evidence: %w", err)
		}
		item.BettingCutoff = nullTimePointer(cutoff)
		item.LastFixtureObserved = nullTimePointer(fixtureObserved)
		item.LastOddsObserved = nullTimePointer(oddsObserved)
		item.RunID = nullStringPointer(runID)
		item.ModelVersion = nullStringPointer(modelVersion)
		item.PredictionStatus = nullStringPointer(predictionStatus)
		item.PredictionAsOf = nullTimePointer(predictionAsOf)
		item.FrozenAt = nullTimePointer(frozenAt)
		item.CompletePreCutoff = item.GeneratedFixtures == 8 && item.FrozenAt != nil && item.FrozenAt.Before(item.EarliestKickoff)
		item.CutoffViolation = item.FrozenAt != nil && !item.FrozenAt.Before(item.EarliestKickoff)
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadSettlements(ctx context.Context, db *sql.DB, result *settlementSummary) error {
	var logLoss, brier, accuracy, stake, returned, profit sql.NullFloat64
	if err := db.QueryRowContext(ctx, `
	SELECT count(*)
FROM prediction_settlements s
JOIN prediction_revisions r ON r.prediction_revision_id = s.prediction_revision_id
JOIN prediction_runs p ON p.prediction_run_id = r.prediction_run_id
	WHERE s.settlement_track = 'forward_observed'`).Scan(&result.RecordedForwardSettlementCount); err != nil {
		return fmt.Errorf("load forward settlements: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT avg(s.log_loss)::double precision, avg(s.brier_score)::double precision,
       avg(CASE WHEN s.correct THEN 1.0 ELSE 0.0 END), count(*) FILTER (WHERE s.quoted_decimal_odds IS NOT NULL),
       sum(s.hypothetical_stake)::double precision, sum(s.hypothetical_return)::double precision,
       sum(s.hypothetical_profit)::double precision
FROM prediction_settlements s
JOIN prediction_revisions r ON r.prediction_revision_id = s.prediction_revision_id
JOIN prediction_runs p ON p.prediction_run_id = r.prediction_run_id
WHERE s.settlement_track = 'forward_observed'
  AND p.frozen_at IS NOT NULL AND p.frozen_at < p.earliest_kickoff_at
  AND EXISTS (
      SELECT 1 FROM live_feed_observations l
      WHERE l.endpoint_family LIKE 'live_%'
        AND l.season_id = p.season_id
        AND (l.round_no IS NULL OR l.round_no = p.round_no)
  )`).Scan(&logLoss, &brier, &accuracy, &result.EligibleOddsCount, &stake, &returned, &profit); err != nil {
		return fmt.Errorf("load provider-backed forward metrics: %w", err)
	}
	result.AverageLogLoss = nullFloatPointer(logLoss)
	result.AverageBrierScore = nullFloatPointer(brier)
	result.Accuracy = nullFloatPointer(accuracy)
	result.HypotheticalStake = nullFloatPointer(stake)
	result.HypotheticalReturn = nullFloatPointer(returned)
	result.HypotheticalProfit = nullFloatPointer(profit)
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM prediction_settlements s
JOIN prediction_revisions r ON r.prediction_revision_id = s.prediction_revision_id
JOIN prediction_runs p ON p.prediction_run_id = r.prediction_run_id
WHERE s.settlement_track = 'forward_observed'
  AND p.frozen_at IS NOT NULL AND p.frozen_at < p.earliest_kickoff_at
  AND EXISTS (
      SELECT 1 FROM live_feed_observations l
      WHERE l.endpoint_family LIKE 'live_%'
        AND l.season_id = p.season_id
        AND (l.round_no IS NULL OR l.round_no = p.round_no)
  )`).Scan(&result.ProviderBackedSettlementCount); err != nil {
		return fmt.Errorf("load provider-backed settlement count: %w", err)
	}
	result.UnclassifiedOrSyntheticCount = result.RecordedForwardSettlementCount - result.ProviderBackedSettlementCount
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM forward_paper_results`).Scan(&result.ForwardMetricRows); err != nil {
		return fmt.Errorf("load forward metric rows: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM prediction_settlements WHERE settlement_track = 'historical_retrospective'`).Scan(&result.RetrospectiveRows); err != nil {
		return fmt.Errorf("load retrospective settlement rows: %w", err)
	}
	return nil
}

func loadJobs(ctx context.Context, db *sql.DB) ([]jobSummary, error) {
	rows, err := db.QueryContext(ctx, `
SELECT job_key, status, count(*), max(started_at), max(finished_at)
FROM job_execution_history
GROUP BY job_key, status
ORDER BY job_key, status`)
	if err != nil {
		return nil, fmt.Errorf("load job summary: %w", err)
	}
	defer rows.Close()
	result := make([]jobSummary, 0)
	for rows.Next() {
		var item jobSummary
		var started, finished sql.NullTime
		if err := rows.Scan(&item.JobKey, &item.Status, &item.Count, &started, &finished); err != nil {
			return nil, fmt.Errorf("scan job summary: %w", err)
		}
		item.LastStarted = nullTimePointer(started)
		item.LastFinished = nullTimePointer(finished)
		result = append(result, item)
	}
	return result, rows.Err()
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullFloatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "phase36-report:", message)
	os.Exit(1)
}
