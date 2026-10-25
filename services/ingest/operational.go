package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LiveObservationInput records the application observation of one provider
// response.  FetchedAt/ObservedAt are local observation times; provider times
// are copied from the payload only as evidence and are not substituted for
// them.
type LiveObservationInput struct {
	EndpointFamily string
	RequestKey     string
	RequestURL     string
	Response       *Response
	RawPayloadID   string
	CacheReused    bool
	SeasonID       string
	Round          int
	ParseStatus    string
	Details        string
}

// SaveObservation is a compact adapter used by live workers. It keeps raw
// payload insertion and observation insertion as two explicit operations while
// avoiding duplicate provenance code in each endpoint worker.
func (r *Repository) SaveObservation(ctx context.Context, endpointFamily, requestKey string, response *Response, rawPayloadID string, cacheReused bool, seasonID string, round int, parseStatus, details string) error {
	_, err := r.SaveLiveObservation(ctx, LiveObservationInput{
		EndpointFamily: endpointFamily,
		RequestKey:     requestKey,
		RequestURL:     response.URL,
		Response:       response,
		RawPayloadID:   rawPayloadID,
		CacheReused:    cacheReused,
		SeasonID:       seasonID,
		Round:          round,
		ParseStatus:    parseStatus,
		Details:        details,
	})
	return err
}

func (r *Repository) SaveLiveObservation(ctx context.Context, input LiveObservationInput) (string, error) {
	if r == nil || r.DB == nil {
		return "", errors.New("repository database is nil")
	}
	if input.Response == nil {
		return "", errors.New("live observation response is nil")
	}
	if strings.TrimSpace(input.RawPayloadID) == "" {
		return "", errors.New("live observation raw payload ID is empty")
	}
	parseStatus := strings.TrimSpace(input.ParseStatus)
	if parseStatus == "" {
		parseStatus = "accepted"
	}
	switch parseStatus {
	case "accepted", "not_modified", "invalid", "unavailable":
	default:
		return "", fmt.Errorf("invalid live observation parse status %q", parseStatus)
	}
	details := strings.TrimSpace(input.Details)
	if details == "" {
		details = `{}`
	}
	if !json.Valid([]byte(details)) {
		return "", errors.New("live observation details are not valid JSON")
	}
	fetchedAt := input.Response.FetchedAt.UTC()
	if fetchedAt.IsZero() {
		fetchedAt = r.now()
	}
	hash := sha256.Sum256(input.Response.Body)
	payloadHash := hex.EncodeToString(hash[:])
	bornUTS, bornAt, timestampMS, timestampAt := inferProviderTimes(input.Response.Body)
	var roundValue any
	if input.Round > 0 {
		roundValue = input.Round
	}
	var observationID string
	err := r.DB.QueryRowContext(ctx, `
INSERT INTO live_feed_observations (
    endpoint_family, request_key, request_url, response_status, fetched_at, observed_at,
    provider_born_uts, provider_born_at, provider_timestamp_ms, provider_timestamp_at,
    payload_sha256, raw_payload_id, response_bytes, cache_reused, cache_control, etag,
    last_modified, season_id, round_no, parse_status, details
) VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9, $10, $11::uuid, $12, $13, NULLIF($14, ''),
          NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''), $18, $19, $20::jsonb)
ON CONFLICT (raw_payload_id, endpoint_family, request_key, observed_at) DO UPDATE SET
    response_status = EXCLUDED.response_status,
    fetched_at = GREATEST(live_feed_observations.fetched_at, EXCLUDED.fetched_at),
    observed_at = GREATEST(live_feed_observations.observed_at, EXCLUDED.observed_at),
    response_bytes = EXCLUDED.response_bytes,
    cache_reused = EXCLUDED.cache_reused,
    cache_control = COALESCE(EXCLUDED.cache_control, live_feed_observations.cache_control),
    etag = COALESCE(EXCLUDED.etag, live_feed_observations.etag),
    last_modified = COALESCE(EXCLUDED.last_modified, live_feed_observations.last_modified),
    season_id = COALESCE(EXCLUDED.season_id, live_feed_observations.season_id),
    round_no = COALESCE(EXCLUDED.round_no, live_feed_observations.round_no),
    parse_status = EXCLUDED.parse_status,
    details = EXCLUDED.details
RETURNING observation_id::text`, input.EndpointFamily, input.RequestKey, input.RequestURL,
		input.Response.StatusCode, fetchedAt, bornUTS, bornAt, timestampMS, timestampAt,
		payloadHash, input.RawPayloadID, len(input.Response.Body), input.CacheReused,
		input.Response.CacheControl, input.Response.ETag, input.Response.LastModified,
		input.SeasonID, roundValue, parseStatus, details).Scan(&observationID)
	if err != nil {
		return "", fmt.Errorf("save live observation: %w", err)
	}
	return observationID, nil
}

type liveOddsRow struct {
	ID             string
	RawPayloadID   string
	ProviderMarket int64
	Specifiers     string
	OutcomeID      string
	Decimal        sql.NullFloat64
	Valid          bool
	Active         int64
	MarketStatus   int
	ObservedAt     time.Time
	SourceMillis   int64
}

// DetectAndRecordOddsChanges compares the latest live rows for a match with
// the immediately preceding row for each market/outcome key.  Only an initial
// live observation or a change in price/availability flags is recorded.
func (r *Repository) DetectAndRecordOddsChanges(ctx context.Context, matchID int64, observedAt time.Time, rawPayloadID string) (int, error) {
	if r == nil || r.DB == nil {
		return 0, errors.New("repository database is nil")
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT odds_snapshot_id::text, COALESCE(raw_payload_id::text, ''), provider_market_id,
       specifiers, outcome_id, decimal_odds::double precision, odds_valid, outcome_active,
       market_status, last_observed_at, source_timestamp_ms
FROM odds_snapshots
WHERE match_id = $1 AND observation_kind = 'live_observation' AND last_observed_at <= $2
ORDER BY provider_market_id, specifiers, outcome_id, last_observed_at DESC,
         source_timestamp_ms DESC, odds_snapshot_id DESC`, matchID, observedAt.UTC())
	if err != nil {
		return 0, fmt.Errorf("load live odds history: %w", err)
	}
	defer rows.Close()
	byKey := make(map[string][]liveOddsRow)
	for rows.Next() {
		var row liveOddsRow
		if err := rows.Scan(&row.ID, &row.RawPayloadID, &row.ProviderMarket, &row.Specifiers,
			&row.OutcomeID, &row.Decimal, &row.Valid, &row.Active, &row.MarketStatus,
			&row.ObservedAt, &row.SourceMillis); err != nil {
			return 0, fmt.Errorf("scan live odds history: %w", err)
		}
		key := fmt.Sprintf("%d|%s|%s", row.ProviderMarket, row.Specifiers, row.OutcomeID)
		if len(byKey[key]) < 2 {
			byKey[key] = append(byKey[key], row)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	changes := 0
	for _, history := range byKey {
		current := history[0]
		if !current.Valid || !current.Decimal.Valid || current.Decimal.Float64 <= 0 {
			continue
		}
		changeType := "initial"
		var previous *liveOddsRow
		if len(history) > 1 {
			candidate := history[1]
			previous = &candidate
			priceChanged := !candidate.Decimal.Valid || !current.Decimal.Valid || candidate.Decimal.Float64 != current.Decimal.Float64
			availabilityChanged := candidate.Valid != current.Valid || candidate.Active != current.Active || candidate.MarketStatus != current.MarketStatus
			if !priceChanged && !availabilityChanged {
				continue
			}
			if priceChanged {
				changeType = "price_changed"
			} else {
				changeType = "availability_changed"
			}
		}
		var alreadyRecorded bool
		if err := r.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM odds_change_history WHERE current_odds_snapshot_id = $1::uuid AND change_type = $2)`, current.ID, changeType).Scan(&alreadyRecorded); err != nil {
			return changes, err
		}
		if alreadyRecorded {
			continue
		}
		var previousID any
		var previousDecimal any
		if previous != nil {
			previousID = previous.ID
			if previous.Decimal.Valid && previous.Decimal.Float64 > 0 {
				previousDecimal = previous.Decimal.Float64
			}
		}
		_, err := r.DB.ExecContext(ctx, `
INSERT INTO odds_change_history (
    match_id, provider_market_id, specifiers, outcome_id, previous_odds_snapshot_id,
    current_odds_snapshot_id, previous_decimal_odds, current_decimal_odds, observed_at,
    change_type, meaningful, raw_payload_id, provenance
) VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6::uuid, $7, $8, $9, $10, true,
          NULLIF($11, '')::uuid, $12::jsonb)
ON CONFLICT (current_odds_snapshot_id, previous_odds_snapshot_id, change_type) DO NOTHING`,
			matchID, current.ProviderMarket, current.Specifiers, current.OutcomeID,
			stringValue(previousID), current.ID, previousDecimal, current.Decimal.Float64,
			current.ObservedAt.UTC(), changeType, rawPayloadID,
			`{"source":"live_observation","point_in_time_verified":"stored per odds snapshot; post-kickoff changes are non-executable"}`)
		if err != nil {
			return changes, fmt.Errorf("record odds change: %w", err)
		}
		changes++
	}
	return changes, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// AcquireLease atomically claims a lease when it is expired or already owned
// by owner.  A zero/negative TTL is rejected so callers cannot accidentally
// create a lease that is immediately stale.
func (r *Repository) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (string, bool, error) {
	if ttl <= 0 {
		return "", false, errors.New("lease TTL must be positive")
	}
	var token string
	err := r.DB.QueryRowContext(ctx, `
INSERT INTO scheduler_leases (lease_key, owner_id, lease_until, heartbeat_at)
VALUES ($1, $2, now() + $3::interval, now())
ON CONFLICT (lease_key) DO UPDATE SET
    owner_id = EXCLUDED.owner_id,
    lease_until = now() + $3::interval,
    heartbeat_at = now()
WHERE scheduler_leases.lease_until <= now() OR scheduler_leases.owner_id = EXCLUDED.owner_id
RETURNING lease_token::text`, key, owner, ttl.String()).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("acquire lease %s: %w", key, err)
	}
	return token, true, nil
}

func (r *Repository) RefreshLease(ctx context.Context, key, owner, token string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("lease TTL must be positive")
	}
	result, err := r.DB.ExecContext(ctx, `
UPDATE scheduler_leases
SET lease_until = now() + $4::interval, heartbeat_at = now()
WHERE lease_key = $1 AND owner_id = $2 AND lease_token = $3::uuid`, key, owner, token, ttl.String())
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *Repository) ReleaseLease(ctx context.Context, key, owner, token string) error {
	_, err := r.DB.ExecContext(ctx, `DELETE FROM scheduler_leases WHERE lease_key = $1 AND owner_id = $2 AND lease_token = $3::uuid`, key, owner, token)
	return err
}

// LeaseStatus is an inspection view of one scheduler lease. DatabaseNow is
// read from PostgreSQL so an operator does not make a reclaim decision using
// a clock that may differ from the database clock.
type LeaseStatus struct {
	LeaseKey          string    `json:"lease_key"`
	OwnerID           string    `json:"owner_id,omitempty"`
	LeaseToken        string    `json:"lease_token,omitempty"`
	AcquiredAt        time.Time `json:"acquired_at,omitempty"`
	LeaseUntil        time.Time `json:"lease_until,omitempty"`
	HeartbeatAt       time.Time `json:"heartbeat_at,omitempty"`
	DatabaseNow       time.Time `json:"database_now"`
	Metadata          string    `json:"metadata,omitempty"`
	Exists            bool      `json:"exists"`
	Expired           bool      `json:"expired"`
	CancelledJobCount int       `json:"cancelled_job_count,omitempty"`
}

// GetLease returns the current database-backed lease state without changing
// it. A missing lease is represented by Exists=false and is not an error.
func (r *Repository) GetLease(ctx context.Context, key string) (LeaseStatus, error) {
	if r == nil || r.DB == nil {
		return LeaseStatus{}, errors.New("repository database is nil")
	}
	status := LeaseStatus{LeaseKey: key}
	var metadata []byte
	err := r.DB.QueryRowContext(ctx, `
SELECT lease_key, owner_id, lease_token::text, acquired_at, lease_until,
       heartbeat_at, metadata, now()
FROM scheduler_leases
WHERE lease_key = $1`, key).Scan(
		&status.LeaseKey, &status.OwnerID, &status.LeaseToken, &status.AcquiredAt,
		&status.LeaseUntil, &status.HeartbeatAt, &metadata, &status.DatabaseNow,
	)
	if errors.Is(err, sql.ErrNoRows) {
		status.DatabaseNow = r.now().UTC()
		return status, nil
	}
	if err != nil {
		return LeaseStatus{}, fmt.Errorf("inspect lease %s: %w", key, err)
	}
	status.Metadata = string(metadata)
	status.Exists = true
	status.Expired = !status.LeaseUntil.After(status.DatabaseNow)
	return status, nil
}

// ReclaimExpiredLease removes a lease only when PostgreSQL says that its
// lease_until has passed. It is intentionally atomic and cannot delete a
// valid lease or a lease that changed between inspection and reclamation.
// The returned LeaseStatus describes the reclaimed owner/token when reclaimed
// is true. When false, the current lease is returned unchanged.
func (r *Repository) ReclaimExpiredLease(ctx context.Context, key string) (status LeaseStatus, reclaimed bool, err error) {
	if r == nil || r.DB == nil {
		return LeaseStatus{}, false, errors.New("repository database is nil")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return LeaseStatus{}, false, fmt.Errorf("begin lease reclaim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var metadata []byte
	err = tx.QueryRowContext(ctx, `
DELETE FROM scheduler_leases
WHERE lease_key = $1 AND lease_until <= now()
RETURNING lease_key, owner_id, lease_token::text, acquired_at, lease_until,
          heartbeat_at, metadata, now()`, key).Scan(
		&status.LeaseKey, &status.OwnerID, &status.LeaseToken, &status.AcquiredAt,
		&status.LeaseUntil, &status.HeartbeatAt, &metadata, &status.DatabaseNow,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if commitErr := tx.Commit(); commitErr != nil {
			return LeaseStatus{}, false, fmt.Errorf("finish lease reclaim inspection: %w", commitErr)
		}
		current, inspectErr := r.GetLease(ctx, key)
		return current, false, inspectErr
	}
	if err != nil {
		return LeaseStatus{}, false, fmt.Errorf("reclaim expired lease %s: %w", key, err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE job_execution_history
SET status = 'cancelled', finished_at = now(), error_code = 'lease_expired',
    error_message = 'job cancelled while reclaiming an expired scheduler lease'
WHERE job_key = $1 AND status = 'running' AND finished_at IS NULL`, key)
	if err != nil {
		return LeaseStatus{}, false, fmt.Errorf("cancel jobs for expired lease %s: %w", key, err)
	}
	cancelledJobs, err := result.RowsAffected()
	if err != nil {
		return LeaseStatus{}, false, fmt.Errorf("count cancelled jobs for expired lease %s: %w", key, err)
	}
	if err := tx.Commit(); err != nil {
		return LeaseStatus{}, false, fmt.Errorf("commit expired lease reclaim: %w", err)
	}
	status.Metadata = string(metadata)
	status.Exists = true
	status.Expired = true
	status.CancelledJobCount = int(cancelledJobs)
	return status, true, nil
}

// ReconcileOrphanedRunningJobs marks old running records as cancelled only
// when no valid lease for the same job key exists. It is an explicit operator
// recovery operation for a process that disappeared after its lease row was
// already removed; a valid owner prevents the update.
func (r *Repository) ReconcileOrphanedRunningJobs(ctx context.Context, key string, minimumAge time.Duration) (int, error) {
	if r == nil || r.DB == nil {
		return 0, errors.New("repository database is nil")
	}
	if minimumAge <= 0 {
		return 0, errors.New("orphan job minimum age must be positive")
	}
	result, err := r.DB.ExecContext(ctx, `
UPDATE job_execution_history
SET status = 'cancelled', finished_at = now(), error_code = 'orphaned_job',
    error_message = 'job cancelled because no valid scheduler lease remains'
WHERE job_key = $1 AND status = 'running' AND finished_at IS NULL
  AND started_at <= now() - $2::interval
  AND NOT EXISTS (
      SELECT 1 FROM scheduler_leases
      WHERE lease_key = $1 AND lease_until > now()
  )`, key, minimumAge.String())
	if err != nil {
		return 0, fmt.Errorf("reconcile orphaned jobs for %s: %w", key, err)
	}
	count, err := result.RowsAffected()
	return int(count), err
}

// AttachDatabaseRequestGate gives independent worker processes one shared
// provider-request slot stream. The local budget is disabled because the
// database reservation already enforces the configured spacing globally.
func AttachDatabaseRequestGate(client *ProviderClient, repo *Repository, spacing time.Duration) {
	if client == nil || repo == nil {
		return
	}
	client.Budget = NewRequestBudget(0)
	client.RequestGate = func(ctx context.Context) error {
		return repo.WaitForProviderRequest(ctx, "bet9ja-global", spacing)
	}
}

func (r *Repository) WaitForProviderRequest(ctx context.Context, budgetKey string, spacing time.Duration) error {
	if r == nil || r.DB == nil {
		return errors.New("repository database is nil")
	}
	if spacing < 0 {
		return errors.New("provider request spacing must not be negative")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
INSERT INTO provider_request_budget (budget_key, next_allowed_at, last_reserved_at)
VALUES ($1, now(), now()) ON CONFLICT (budget_key) DO NOTHING`, budgetKey)
	if err != nil {
		return err
	}
	var nextAllowed, databaseNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT next_allowed_at, now() FROM provider_request_budget WHERE budget_key = $1 FOR UPDATE`, budgetKey).Scan(&nextAllowed, &databaseNow); err != nil {
		return err
	}
	sleepFor := time.Duration(0)
	reservationBase := databaseNow
	if nextAllowed.After(databaseNow) {
		sleepFor = nextAllowed.Sub(databaseNow)
		reservationBase = nextAllowed
	}
	newNext := reservationBase.Add(spacing)
	if _, err := tx.ExecContext(ctx, `UPDATE provider_request_budget SET next_allowed_at = $2, last_reserved_at = $3, updated_at = $3 WHERE budget_key = $1`, budgetKey, newNext, databaseNow); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return sleepContext(ctx, sleepFor)
}

type JobExecution struct {
	ID            string
	JobKey        string
	WorkerName    string
	Status        string
	Attempt       int
	StartedAt     time.Time
	FinishedAt    *time.Time
	ResponseCount int
	CacheHits     int
	ErrorCode     string
	ErrorMessage  string
	Retryable     *bool
	Details       string
}

func (r *Repository) StartJobExecution(ctx context.Context, jobKey, worker string, attempt int, details string) (string, error) {
	if attempt <= 0 {
		attempt = 1
	}
	if strings.TrimSpace(details) == "" {
		details = `{}`
	}
	var id string
	err := r.DB.QueryRowContext(ctx, `
INSERT INTO job_execution_history (job_key, worker_name, status, attempt, started_at, details)
VALUES ($1, $2, 'running', $3, now(), $4::jsonb)
RETURNING execution_id::text`, jobKey, worker, attempt, details).Scan(&id)
	return id, err
}

func (r *Repository) FinishJobExecution(ctx context.Context, executionID, status, code, message string, retryable *bool, responseCount, cacheHits int, details string) error {
	if strings.TrimSpace(details) == "" {
		details = `{}`
	}
	_, err := r.DB.ExecContext(ctx, `
UPDATE job_execution_history
SET status = $2, finished_at = now(), error_code = NULLIF($3, ''), error_message = NULLIF($4, ''),
    retryable = $5, response_count = $6, cache_hit_count = $7, details = $8::jsonb
WHERE execution_id = $1::uuid`, executionID, status, code, message, retryable, responseCount, cacheHits, details)
	return err
}

func (r *Repository) UpsertRoundState(ctx context.Context, seasonID string, round int, kickoff, cutoff time.Time, state string, freshness string) error {
	if strings.TrimSpace(freshness) == "" {
		freshness = `{}`
	}
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO round_states (season_id, round_no, earliest_kickoff_at, betting_cutoff_at, state, last_fixture_observed_at, last_odds_observed_at, freshness)
VALUES ($1, $2, $3, NULLIF($4, '')::timestamptz, $5, now(), now(), $6::jsonb)
ON CONFLICT (season_id, round_no) DO UPDATE SET
    earliest_kickoff_at = EXCLUDED.earliest_kickoff_at,
    betting_cutoff_at = EXCLUDED.betting_cutoff_at,
    state = CASE
        WHEN round_states.state = 'settled' THEN 'settled'
        WHEN round_states.state = 'frozen' AND EXCLUDED.state <> 'settled' THEN 'frozen'
        ELSE EXCLUDED.state
    END,
    last_fixture_observed_at = now(),
    last_odds_observed_at = CASE WHEN $7 THEN now() ELSE round_states.last_odds_observed_at END,
    freshness = EXCLUDED.freshness,
    updated_at = now()`, seasonID, round, kickoff.UTC(), nullableTimeString(cutoff), state, freshness, strings.Contains(freshness, "odds"))
	return err
}

func nullableTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
