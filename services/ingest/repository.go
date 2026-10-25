package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Repository struct {
	DB  *sql.DB
	Now func() time.Time
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{DB: db, Now: time.Now}
}

func (r *Repository) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func OpenDatabase(ctx context.Context, databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is empty")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func ApplyMigrations(ctx context.Context, db *sql.DB, migrationsDir string) error {
	if db == nil {
		return errors.New("database is nil")
	}
	paths, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no SQL migrations found in %s", migrationsDir)
	}
	sort.Strings(paths)
	// Several independent workers may start at the same time. Migration 007
	// contains a backfill insert that takes locks on large odds tables, so
	// running the idempotent migration files concurrently can deadlock even
	// when each individual statement is safe. Hold one PostgreSQL session lock
	// for the entire ordered batch and execute every statement on that session.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended('bet9ja-vfl:migrations', 0))`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtextextended('bet9ja-vfl:migrations', 0))`)
	}()
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		if _, err := conn.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("apply migration %s: %w", path, err)
		}
	}
	return nil
}

type RawPayloadInput struct {
	EndpointFamily string
	RequestKey     string
	RequestURL     string
	RequestParams  string
	Response       *Response
	Payload        []byte
	Provenance     string
}

type StoredRawPayload struct {
	ID           string
	Payload      []byte
	ETag         string
	LastModified string
	FetchedAt    time.Time
}

func (r *Repository) SaveRawPayload(ctx context.Context, input RawPayloadInput) (string, error) {
	if r == nil || r.DB == nil {
		return "", errors.New("repository database is nil")
	}
	if input.Response == nil {
		return "", errors.New("raw payload response is nil")
	}
	params := strings.TrimSpace(input.RequestParams)
	if params == "" {
		params = `{}`
	}
	if !json.Valid([]byte(params)) {
		return "", errors.New("raw request params are not valid JSON")
	}
	if input.Response.StatusCode < 100 || input.Response.StatusCode > 599 {
		return "", fmt.Errorf("invalid response status %d", input.Response.StatusCode)
	}
	hash := sha256.Sum256(input.Payload)
	payloadHash := hex.EncodeToString(hash[:])
	providerBornUTS, providerBornAt, providerTimestampMS, providerTimestampAt := inferProviderTimes(input.Payload)
	provenance := strings.TrimSpace(input.Provenance)
	if provenance == "" {
		provenance = `{}`
	}
	if !json.Valid([]byte(provenance)) {
		return "", errors.New("raw provenance is not valid JSON")
	}
	fetchedAt := input.Response.FetchedAt.UTC()
	if fetchedAt.IsZero() {
		fetchedAt = r.now()
	}
	query := `
INSERT INTO raw_payloads (
    endpoint_family, request_key, request_url, request_params,
    response_status, content_type, cache_control, etag, last_modified,
    provider_born_uts, provider_born_at, provider_timestamp_ms, provider_timestamp_at,
    fetched_at, last_fetched_at, payload_sha256, payload, payload_text, parser_version, schema_version, provenance
) VALUES ($1, $2, $3, $4::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''),
          $10, $11, $12, $13, $14, $14, $15, $16::jsonb, $17, $18, $19, $20::jsonb)
ON CONFLICT (endpoint_family, request_key, payload_sha256) DO UPDATE SET
    response_status = EXCLUDED.response_status,
    content_type = COALESCE(EXCLUDED.content_type, raw_payloads.content_type),
    cache_control = COALESCE(EXCLUDED.cache_control, raw_payloads.cache_control),
    etag = COALESCE(EXCLUDED.etag, raw_payloads.etag),
    last_modified = COALESCE(EXCLUDED.last_modified, raw_payloads.last_modified),
    last_fetched_at = GREATEST(raw_payloads.last_fetched_at, EXCLUDED.last_fetched_at),
    ingested_at = now(),
    provenance = EXCLUDED.provenance
RETURNING raw_payload_id::text`
	var id string
	err := r.DB.QueryRowContext(ctx, query,
		input.EndpointFamily, input.RequestKey, input.RequestURL, params,
		input.Response.StatusCode, input.Response.ContentType, input.Response.CacheControl,
		input.Response.ETag, input.Response.LastModified,
		providerBornUTS, providerBornAt, providerTimestampMS, providerTimestampAt,
		fetchedAt, payloadHash, nullableJSONPayload(input.Payload), string(input.Payload), ParserVersion, SchemaVersion, provenance,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save raw payload: %w", err)
	}
	return id, nil
}

func nullableJSONPayload(payload []byte) any {
	if json.Valid(payload) {
		return string(payload)
	}
	return nil
}

func (r *Repository) LatestRawPayload(ctx context.Context, endpointFamily, requestKey string) (StoredRawPayload, error) {
	var stored StoredRawPayload
	var fetchedAt time.Time
	err := r.DB.QueryRowContext(ctx, `
SELECT raw_payload_id::text, payload::text, COALESCE(etag, ''), COALESCE(last_modified, ''), last_fetched_at
FROM raw_payloads
WHERE endpoint_family = $1 AND request_key = $2 AND response_status BETWEEN 200 AND 299
ORDER BY last_fetched_at DESC
LIMIT 1`, endpointFamily, requestKey).Scan(&stored.ID, &stored.Payload, &stored.ETag, &stored.LastModified, &fetchedAt)
	if err != nil {
		return StoredRawPayload{}, err
	}
	stored.FetchedAt = fetchedAt
	return stored, nil
}

func (r *Repository) UpsertSeason(ctx context.Context, season provider.SeasonSummary, rawPayloadID string, now time.Time) error {
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO seasons (
    season_id, provider_season_id, tournament_id, provider_name, abbreviation, provider_year,
    current_flag, starts_at, ends_at, discovered_at, last_seen_at, raw_payload_id
) VALUES ($1, $1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9, $9, NULLIF($10, '')::uuid)
ON CONFLICT (season_id) DO UPDATE SET
    tournament_id = EXCLUDED.tournament_id,
    provider_name = EXCLUDED.provider_name,
    abbreviation = EXCLUDED.abbreviation,
    provider_year = EXCLUDED.provider_year,
    current_flag = EXCLUDED.current_flag,
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    last_seen_at = EXCLUDED.last_seen_at,
    raw_payload_id = COALESCE(EXCLUDED.raw_payload_id, seasons.raw_payload_id)`,
		season.ID.String(), season.TournamentID, season.Name, season.Abbreviation, season.ProviderYear,
		season.Current, season.StartUTC, season.EndUTC, now.UTC(), rawPayloadID)
	return err
}

// ReconcileCurrentSeasonFlags prevents stale discovery rows from leaving more
// than one current season visible to operational readers. The requested and
// returned season IDs remain separately retained in raw payload provenance.
func (r *Repository) ReconcileCurrentSeasonFlags(ctx context.Context, seasonID string) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE seasons SET current_flag = false WHERE current_flag = true`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE seasons SET current_flag = true, last_seen_at = now() WHERE season_id = $1`, seasonID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) UpsertTeam(ctx context.Context, team provider.TeamRef, rawPayloadID string, now time.Time) error {
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO teams (
    team_uid, provider_team_id, name, medium_name, abbreviation, first_seen_at, last_seen_at, raw_payload_id
) VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $6, NULLIF($7, '')::uuid)
ON CONFLICT (team_uid) DO UPDATE SET
    provider_team_id = COALESCE(EXCLUDED.provider_team_id, teams.provider_team_id),
    name = EXCLUDED.name,
    medium_name = EXCLUDED.medium_name,
    abbreviation = EXCLUDED.abbreviation,
    last_seen_at = EXCLUDED.last_seen_at,
    raw_payload_id = COALESCE(EXCLUDED.raw_payload_id, teams.raw_payload_id)`,
		team.UID, team.ProviderID, team.Name, team.MediumName, team.Abbreviation, now.UTC(), rawPayloadID)
	return err
}

func (r *Repository) UpsertFixture(ctx context.Context, fixture provider.Fixture, rawPayloadID string, now time.Time, confirmationWindow time.Duration) error {
	status := fixture.StatusAsOf(now, confirmationWindow)
	providerStatus := strings.TrimSpace(string(fixture.ProviderStatus))
	if providerStatus == "" {
		providerStatus = "null"
	}
	var resultHome, resultAway, halfHome, halfAway, fullHome, fullAway any
	if fixture.Score != nil {
		resultHome, resultAway = fixture.Score.Home, fixture.Score.Away
	}
	if fixture.HalfTimeScore != nil {
		halfHome, halfAway = fixture.HalfTimeScore.Home, fixture.HalfTimeScore.Away
	}
	if fixture.FullTimeScore != nil {
		fullHome, fullAway = fixture.FullTimeScore.Home, fixture.FullTimeScore.Away
	}
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO fixtures (
    match_id, provider_match_id, season_id, tournament_id, round_no, scheduled_at, provider_scheduled_uts,
    home_team_uid, away_team_uid, status, result_home, result_away, half_time_home, half_time_away,
    full_time_home, full_time_away, provider_status, postponed, canceled, walkover, to_be_announced,
    in_live_score, status_observed_at, raw_payload_id, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
          $17::jsonb, $18, $19, $20, $21, $22, $23, NULLIF($24, '')::uuid, $23)
ON CONFLICT (match_id) DO UPDATE SET
    provider_match_id = EXCLUDED.provider_match_id,
    season_id = EXCLUDED.season_id,
    tournament_id = EXCLUDED.tournament_id,
    round_no = EXCLUDED.round_no,
    scheduled_at = EXCLUDED.scheduled_at,
    provider_scheduled_uts = EXCLUDED.provider_scheduled_uts,
    home_team_uid = EXCLUDED.home_team_uid,
    away_team_uid = EXCLUDED.away_team_uid,
    status = EXCLUDED.status,
    result_home = EXCLUDED.result_home,
    result_away = EXCLUDED.result_away,
    half_time_home = EXCLUDED.half_time_home,
    half_time_away = EXCLUDED.half_time_away,
    full_time_home = EXCLUDED.full_time_home,
    full_time_away = EXCLUDED.full_time_away,
    provider_status = EXCLUDED.provider_status,
    postponed = EXCLUDED.postponed,
    canceled = EXCLUDED.canceled,
    walkover = EXCLUDED.walkover,
    to_be_announced = EXCLUDED.to_be_announced,
    in_live_score = EXCLUDED.in_live_score,
    status_observed_at = EXCLUDED.status_observed_at,
    raw_payload_id = COALESCE(EXCLUDED.raw_payload_id, fixtures.raw_payload_id),
    updated_at = EXCLUDED.updated_at`,
		fixture.MatchID, fixture.ProviderMatchID, fixture.SeasonID.String(), fixture.TournamentID,
		fixture.Round.Int(), fixture.ScheduledAt.UTC(), fixture.ScheduledUTS, fixture.Home.UID,
		fixture.Away.UID, string(status), resultHome, resultAway, halfHome, halfAway, fullHome, fullAway,
		providerStatus, fixture.Postponed, fixture.Canceled, fixture.Walkover, fixture.ToBeAnnounced,
		fixture.InLiveScore, now.UTC(), rawPayloadID)
	return err
}

type OddsPersistResult struct {
	Accepted    int
	Quarantined int
	JoinFailure int
}

// LiveOddsPointInTimeVerified is intentionally strict: the application's
// observation must precede the fixture kickoff. A provider source timestamp
// cannot make a post-kickoff response pre-match eligible.
func LiveOddsPointInTimeVerified(fixture provider.Fixture, fetchedAt time.Time) bool {
	return !fetchedAt.IsZero() && fetchedAt.UTC().Before(fixture.ScheduledAt.UTC())
}

func (r *Repository) UpsertOddsSnapshot(ctx context.Context, snapshot provider.OddsSnapshot, fixtures map[provider.MatchID]provider.Fixture, rawPayloadID string, fetchedAt time.Time, observationKind string, provenance string, jobID, checkpointKey string) (OddsPersistResult, error) {
	return r.upsertOddsSnapshot(ctx, snapshot, fixtures, rawPayloadID, fetchedAt, observationKind, false, provenance, jobID, checkpointKey)
}

// UpsertLiveOddsSnapshot is the live-feed variant of UpsertOddsSnapshot.  Its
// point_in_time_verified flag is true only because fetchedAt is an observation
// made by this application while the fixture was pending; provider timestamps
// alone never enable this flag.
func (r *Repository) UpsertLiveOddsSnapshot(ctx context.Context, snapshot provider.OddsSnapshot, fixtures map[provider.MatchID]provider.Fixture, rawPayloadID string, fetchedAt time.Time, provenance string, jobID, checkpointKey string) (OddsPersistResult, error) {
	return r.upsertOddsSnapshot(ctx, snapshot, fixtures, rawPayloadID, fetchedAt, "live_observation", true, provenance, jobID, checkpointKey)
}

func (r *Repository) upsertOddsSnapshot(ctx context.Context, snapshot provider.OddsSnapshot, fixtures map[provider.MatchID]provider.Fixture, rawPayloadID string, fetchedAt time.Time, observationKind string, pointInTimeVerified bool, provenance string, jobID, checkpointKey string) (OddsPersistResult, error) {
	if strings.TrimSpace(provenance) == "" {
		provenance = `{}`
	}
	if !json.Valid([]byte(provenance)) {
		return OddsPersistResult{}, errors.New("odds provenance is not valid JSON")
	}
	if fetchedAt.IsZero() {
		fetchedAt = r.now()
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return OddsPersistResult{}, fmt.Errorf("begin odds transaction: %w", err)
	}
	result := OddsPersistResult{}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, record := range snapshot.Records {
		fixture, exists := fixtures[record.MatchID]
		if !exists {
			result.JoinFailure++
			result.Quarantined++
			if err = insertValidationErrorTx(ctx, tx, jobID, rawPayloadID, "odds_record", record.MatchID.String(), "odds_fixture_join_failed", "odds match ID has no fixture in the requested season", `{}`); err != nil {
				return result, err
			}
			continue
		}
		if fixture.Home.UID != record.HomeTeamUID || fixture.Away.UID != record.AwayTeamUID {
			result.Quarantined++
			if err = insertValidationErrorTx(ctx, tx, jobID, rawPayloadID, "odds_record", record.MatchID.String(), "odds_team_join_failed", "odds team IDs do not match fixture home/away IDs", `{}`); err != nil {
				return result, err
			}
			continue
		}
		verifiedForFixture := pointInTimeVerified && LiveOddsPointInTimeVerified(fixture, fetchedAt)
		for _, market := range record.Markets {
			var marketDefinitionID int64
			err = tx.QueryRowContext(ctx, `
INSERT INTO market_definitions (provider_market_id, market_name)
VALUES ($1, $2)
ON CONFLICT (provider_market_id, market_name) DO UPDATE SET market_name = EXCLUDED.market_name
RETURNING market_definition_id`, market.ID, market.Name).Scan(&marketDefinitionID)
			if err != nil {
				return result, fmt.Errorf("upsert market definition %d/%q: %w", market.ID, market.Name, err)
			}
			for _, outcome := range market.Outcomes {
				var decimalOdds any
				oddsValid := outcome.IsUsable()
				if oddsValid {
					decimalOdds = outcome.Odds.String()
				}
				oddsError := outcome.OddsError
				if oddsError == "" {
					oddsError = ""
				}
				var oddsSnapshotID string
				err = tx.QueryRowContext(ctx, `
INSERT INTO odds_snapshots (
    match_id, market_definition_id, provider_market_id, specifiers, outcome_id, outcome_name,
    outcome_abbreviation, raw_odds, decimal_odds, odds_valid, odds_error, market_status,
    outcome_active, provider_result, source_timestamp_ms, source_timestamp_at, first_observed_at,
    last_observed_at, fetched_at, observation_kind, point_in_time_verified, raw_payload_id, provenance
) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, NULLIF($11, ''), $12, $13, $14,
          $15, $16, $17, $17, $17, $18, $19, $20::uuid, $21::jsonb)
ON CONFLICT (match_id, provider_market_id, specifiers, outcome_id, source_timestamp_ms, raw_odds) DO UPDATE SET
    market_definition_id = EXCLUDED.market_definition_id,
    outcome_name = EXCLUDED.outcome_name,
    outcome_abbreviation = EXCLUDED.outcome_abbreviation,
    decimal_odds = EXCLUDED.decimal_odds,
    odds_valid = EXCLUDED.odds_valid,
    odds_error = EXCLUDED.odds_error,
    market_status = EXCLUDED.market_status,
    outcome_active = EXCLUDED.outcome_active,
    provider_result = EXCLUDED.provider_result,
    observation_kind = EXCLUDED.observation_kind,
    first_observed_at = LEAST(odds_snapshots.first_observed_at, EXCLUDED.first_observed_at),
    last_observed_at = GREATEST(odds_snapshots.last_observed_at, EXCLUDED.last_observed_at),
    fetched_at = GREATEST(odds_snapshots.fetched_at, EXCLUDED.fetched_at),
    point_in_time_verified = odds_snapshots.point_in_time_verified OR EXCLUDED.point_in_time_verified,
    provenance = EXCLUDED.provenance
RETURNING odds_snapshot_id::text`,
					fixture.MatchID, marketDefinitionID, market.ID, market.Specifiers, outcome.ID, outcome.Name,
					outcome.Abbreviation, outcome.RawOdds, decimalOdds, oddsValid, oddsError, market.Status,
					outcome.Active, outcome.ProviderResult, snapshot.SourceTimestampMillis,
					snapshot.SourceTimestampUTC, fetchedAt.UTC(), observationKind, verifiedForFixture, rawPayloadID, provenance).Scan(&oddsSnapshotID)
				if err != nil {
					return result, fmt.Errorf("upsert odds %s/%d/%s: %w", record.MatchID, market.ID, outcome.ID, err)
				}
				_, err = tx.ExecContext(ctx, `
INSERT INTO odds_snapshot_observations (
    odds_snapshot_id, raw_payload_id, observed_at, fetched_at,
    source_timestamp_ms, source_timestamp_at, observation_kind,
    point_in_time_verified, provenance
) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9::jsonb)
ON CONFLICT (odds_snapshot_id, raw_payload_id, observed_at) DO UPDATE SET
    fetched_at = GREATEST(odds_snapshot_observations.fetched_at, EXCLUDED.fetched_at),
    point_in_time_verified = odds_snapshot_observations.point_in_time_verified OR EXCLUDED.point_in_time_verified,
    provenance = EXCLUDED.provenance`,
					oddsSnapshotID, rawPayloadID, fetchedAt.UTC(), fetchedAt.UTC(), snapshot.SourceTimestampMillis,
					snapshot.SourceTimestampUTC, observationKind, verifiedForFixture, provenance)
				if err != nil {
					return result, fmt.Errorf("record odds observation %s/%d/%s: %w", record.MatchID, market.ID, outcome.ID, err)
				}
				if oddsValid {
					result.Accepted++
				} else {
					result.Quarantined++
					if err = insertValidationErrorTx(ctx, tx, jobID, rawPayloadID, "odds_outcome", record.MatchID.String()+"/"+strconv.FormatInt(market.ID, 10)+"/"+outcome.ID, "invalid_decimal_odds", outcome.OddsError, `{}`); err != nil {
						return result, err
					}
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit odds transaction: %w", err)
	}
	return result, nil
}

func (r *Repository) EnsureJob(ctx context.Context, jobType, seasonID string, round int, idempotencyKey string, maxAttempts int) (jobID, status string, attempt int, err error) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var roundValue any
	if round > 0 {
		roundValue = round
	}
	err = r.DB.QueryRowContext(ctx, `
INSERT INTO ingestion_jobs (job_type, season_id, round_no, idempotency_key, status, max_attempts)
VALUES ($1, NULLIF($2, ''), $3, $4, 'pending', $5)
ON CONFLICT (idempotency_key) DO UPDATE SET updated_at = now()
RETURNING job_id::text, status, attempt`, jobType, seasonID, roundValue, idempotencyKey, maxAttempts).Scan(&jobID, &status, &attempt)
	return
}

func (r *Repository) SetJobStatus(ctx context.Context, jobID, status, lastError string) error {
	_, err := r.DB.ExecContext(ctx, `
UPDATE ingestion_jobs
SET status = $2,
    last_error = NULLIF($3, ''),
    attempt = CASE WHEN $2 = 'running' THEN attempt + 1 ELSE attempt END,
    started_at = CASE WHEN $2 = 'running' THEN COALESCE(started_at, now()) ELSE started_at END,
    completed_at = CASE WHEN $2 IN ('completed', 'completed_with_errors', 'failed') THEN now() ELSE completed_at END,
    updated_at = now()
WHERE job_id = $1::uuid`, jobID, status, lastError)
	return err
}

type Checkpoint struct {
	Key              string
	State            string
	Attempt          int
	RequestCount     int
	ExpectedCount    int
	AcceptedCount    int
	QuarantinedCount int
	LastRawPayloadID string
	LastError        string
}

func (r *Repository) GetCheckpoint(ctx context.Context, key string) (Checkpoint, error) {
	var checkpoint Checkpoint
	var rawID sql.NullString
	var lastError sql.NullString
	err := r.DB.QueryRowContext(ctx, `
SELECT checkpoint_key, state, attempt, request_count, COALESCE(expected_count, 0), accepted_count,
       quarantined_count, COALESCE(last_raw_payload_id::text, ''), COALESCE(last_error, '')
FROM ingestion_checkpoints WHERE checkpoint_key = $1`, key).Scan(
		&checkpoint.Key, &checkpoint.State, &checkpoint.Attempt, &checkpoint.RequestCount,
		&checkpoint.ExpectedCount, &checkpoint.AcceptedCount, &checkpoint.QuarantinedCount, &rawID, &lastError)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint.LastRawPayloadID = rawID.String
	checkpoint.LastError = lastError.String
	return checkpoint, nil
}

func (r *Repository) UpsertCheckpoint(ctx context.Context, checkpoint Checkpoint, scope, seasonID string, round int) error {
	var roundValue any
	if round > 0 {
		roundValue = round
	}
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO ingestion_checkpoints (
    checkpoint_key, scope, season_id, round_no, state, attempt, request_count, expected_count,
    accepted_count, quarantined_count, last_raw_payload_id, last_error,
    started_at, completed_at, updated_at
) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, NULLIF($8, 0), $9, $10,
          NULLIF($11, '')::uuid, NULLIF($12, ''),
          CASE WHEN $5 = 'running' THEN now() ELSE NULL END,
          CASE WHEN $5 IN ('completed', 'completed_with_errors', 'failed') THEN now() ELSE NULL END,
          now())
ON CONFLICT (checkpoint_key) DO UPDATE SET
    state = EXCLUDED.state,
    season_id = EXCLUDED.season_id,
    round_no = EXCLUDED.round_no,
    attempt = EXCLUDED.attempt,
    request_count = EXCLUDED.request_count,
    expected_count = EXCLUDED.expected_count,
    accepted_count = EXCLUDED.accepted_count,
    quarantined_count = EXCLUDED.quarantined_count,
    last_raw_payload_id = COALESCE(EXCLUDED.last_raw_payload_id, ingestion_checkpoints.last_raw_payload_id),
    last_error = EXCLUDED.last_error,
    started_at = COALESCE(ingestion_checkpoints.started_at, EXCLUDED.started_at),
    completed_at = EXCLUDED.completed_at,
    updated_at = now()`,
		checkpoint.Key, scope, seasonID, roundValue, checkpoint.State, checkpoint.Attempt,
		checkpoint.RequestCount, checkpoint.ExpectedCount, checkpoint.AcceptedCount, checkpoint.QuarantinedCount,
		checkpoint.LastRawPayloadID, checkpoint.LastError)
	return err
}

func (r *Repository) RecordFailure(ctx context.Context, jobID, checkpointKey string, attempt int, code, message string, retryable bool, status int) error {
	_, err := r.DB.ExecContext(ctx, `
INSERT INTO ingestion_failures (job_id, checkpoint_key, attempt, error_code, message, retryable, response_status)
VALUES (NULLIF($1, '')::uuid, NULLIF($2, ''), $3, $4, $5, $6, NULLIF($7, 0))`, jobID, checkpointKey, attempt, code, message, retryable, status)
	return err
}

func (r *Repository) InsertValidationError(ctx context.Context, jobID, rawPayloadID, entityType, entityKey, code, message, fragment string) error {
	return insertValidationError(ctx, r.DB, jobID, rawPayloadID, entityType, entityKey, code, message, fragment)
}

func insertValidationError(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, jobID, rawPayloadID, entityType, entityKey, code, message, fragment string) error {
	if strings.TrimSpace(fragment) == "" || !json.Valid([]byte(fragment)) {
		fragment = `{}`
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO validation_errors (job_id, raw_payload_id, entity_type, entity_key, error_code, message, raw_fragment)
VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7::jsonb)
ON CONFLICT (raw_payload_id, entity_type, entity_key, error_code) DO UPDATE SET
    message = EXCLUDED.message,
    raw_fragment = EXCLUDED.raw_fragment`, jobID, rawPayloadID, entityType, entityKey, code, message, fragment)
	return err
}

func insertValidationErrorTx(ctx context.Context, tx *sql.Tx, jobID, rawPayloadID, entityType, entityKey, code, message, fragment string) error {
	return insertValidationError(ctx, tx, jobID, rawPayloadID, entityType, entityKey, code, message, fragment)
}

type CoverageRow struct {
	SeasonID             string
	Round                int
	FixtureCount         int
	OddsMatchCount       int
	MarketSnapshotCount  int
	OutcomeSnapshotCount int
	UsableOutcomeCount   int
}

// SeasonCoverageComplete is deliberately stricter than a completed job flag:
// it requires every expected round and all eight fixture/odds match joins.
// A season with quarantined individual outcomes can still be reused because
// the malformed rows remain auditable and do not justify refetching complete
// provider payloads.
func (r *Repository) SeasonCoverageComplete(ctx context.Context, seasonID string) (bool, error) {
	var complete bool
	err := r.DB.QueryRowContext(ctx, `
SELECT count(*) = 30
   AND COALESCE(bool_and(fixture_count = 8 AND odds_match_count = 8), false)
FROM v_odds_coverage
WHERE season_id = $1`, seasonID).Scan(&complete)
	if err != nil {
		return false, fmt.Errorf("check season %s coverage: %w", seasonID, err)
	}
	return complete, nil
}

func (r *Repository) Coverage(ctx context.Context, seasonID string) ([]CoverageRow, error) {
	rows, err := r.DB.QueryContext(ctx, `
	SELECT season_id, round_no, fixture_count, odds_match_count, market_snapshot_count, outcome_snapshot_count, usable_outcome_snapshot_count
FROM v_odds_coverage
WHERE season_id = $1
ORDER BY round_no`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var coverage []CoverageRow
	for rows.Next() {
		var row CoverageRow
		if err := rows.Scan(&row.SeasonID, &row.Round, &row.FixtureCount, &row.OddsMatchCount, &row.MarketSnapshotCount, &row.OutcomeSnapshotCount, &row.UsableOutcomeCount); err != nil {
			return nil, err
		}
		coverage = append(coverage, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return coverage, nil
}

func inferProviderTimes(payload []byte) (bornUTS *int64, bornAt *time.Time, timestampMS *int64, timestampAt *time.Time) {
	var envelope struct {
		Doc []struct {
			Born      json.RawMessage `json:"_dob"`
			Timestamp json.RawMessage `json:"timestamp"`
		} `json:"doc"`
	}
	if json.Unmarshal(payload, &envelope) != nil || len(envelope.Doc) == 0 {
		return nil, nil, nil, nil
	}
	if value, ok := rawInt64(envelope.Doc[0].Born); ok && value > 0 {
		bornUTS = &value
		when := time.Unix(value, 0).UTC()
		bornAt = &when
	}
	if value, ok := rawInt64(envelope.Doc[0].Timestamp); ok && value > 0 {
		timestampMS = &value
		when := time.UnixMilli(value).UTC()
		timestampAt = &when
	}
	return bornUTS, bornAt, timestampMS, timestampAt
}

func rawInt64(raw json.RawMessage) (int64, bool) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0, false
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		return parsed, err == nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}
