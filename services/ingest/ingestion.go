package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
)

type Runner struct {
	Config Config
	Client *ProviderClient
	Repo   *Repository
	Now    func() time.Time
}

type BackfillOptions struct {
	SeasonID   string
	MaxSeasons int
	DryRun     bool
}

type SeasonReport struct {
	SeasonID             string
	FixtureCount         int
	FixtureRoundCount    int
	FixtureValidationErr int
	OddsRoundsAttempted  int
	OddsRoundsCompleted  int
	OddsRoundsWithErrors int
	AcceptedOutcomes     int
	QuarantinedRecords   int
	JoinFailures         int
	Errors               []string
	Coverage             []CoverageRow
}

type BackfillReport struct {
	DiscoveredSeasons         int
	CompletedSeasonsAvailable int
	SelectedSeasonIDs         []string
	ReusedSeasonIDs           []string
	DeferredCompletedSeasons  int
	Seasons                   []SeasonReport
}

func NewRunner(cfg Config, client *ProviderClient, repo *Repository) *Runner {
	return &Runner{Config: cfg, Client: client, Repo: repo, Now: time.Now}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) Run(ctx context.Context, options BackfillOptions) (BackfillReport, error) {
	if r == nil || r.Client == nil || r.Repo == nil {
		return BackfillReport{}, errors.New("ingestion runner is not initialized")
	}
	if !r.Config.ConnectivityEnabled && !options.DryRun {
		return BackfillReport{}, errors.New("provider connectivity is disabled; set BET9JA_CONNECTIVITY_ENABLED=true only for an authorized read-only run")
	}
	if options.MaxSeasons <= 0 {
		options.MaxSeasons = r.Config.MaxSeasons
	}
	if options.MaxSeasons <= 0 {
		options.MaxSeasons = 1
	}
	if options.MaxSeasons > 30 {
		return BackfillReport{}, errors.New("refusing more than 30 seasons in one bounded Phase 3 run")
	}

	discovery, rawPayloadID, err := r.discoverSeasons(ctx)
	if err != nil {
		return BackfillReport{}, err
	}
	completedAvailable := countCompletedSeasons(discovery, r.now())
	selected, reused, err := r.selectRunSeasons(ctx, discovery, options, r.now())
	if err != nil {
		return BackfillReport{}, err
	}
	if options.DryRun {
		_ = rawPayloadID
		report := BackfillReport{
			DiscoveredSeasons:         len(discovery.Seasons),
			CompletedSeasonsAvailable: completedAvailable,
			SelectedSeasonIDs:         make([]string, 0, len(selected)),
			ReusedSeasonIDs:           reused,
			DeferredCompletedSeasons:  deferredCompletedCount(completedAvailable, len(selected), len(reused)),
			Seasons:                   make([]SeasonReport, 0, len(selected)),
		}
		for _, season := range selected {
			report.SelectedSeasonIDs = append(report.SelectedSeasonIDs, season.ID.String())
			report.Seasons = append(report.Seasons, SeasonReport{SeasonID: season.ID.String()})
		}
		return report, nil
	}
	report := BackfillReport{
		DiscoveredSeasons:         len(discovery.Seasons),
		CompletedSeasonsAvailable: completedAvailable,
		SelectedSeasonIDs:         make([]string, 0, len(selected)),
		ReusedSeasonIDs:           reused,
		DeferredCompletedSeasons:  deferredCompletedCount(completedAvailable, len(selected), len(reused)),
		Seasons:                   make([]SeasonReport, 0, len(selected)),
	}
	for _, season := range selected {
		seasonReport, err := r.IngestSeason(ctx, season, rawPayloadID)
		if err != nil {
			return report, err
		}
		report.SelectedSeasonIDs = append(report.SelectedSeasonIDs, season.ID.String())
		report.Seasons = append(report.Seasons, seasonReport)
	}
	return report, nil
}

func deferredCompletedCount(available, selected, reused int) int {
	deferred := available - selected - reused
	if deferred < 0 {
		return 0
	}
	return deferred
}

// selectRunSeasons skips already complete seasons for an automatic batch. An
// explicit -season-id remains selectable so an operator can inspect or retry
// that season; IngestSeason still uses its idempotent job/checkpoint behavior.
func (r *Runner) selectRunSeasons(ctx context.Context, discovery provider.SeasonDiscovery, options BackfillOptions, now time.Time) ([]provider.SeasonSummary, []string, error) {
	if strings.TrimSpace(options.SeasonID) != "" {
		selected, err := selectSeasons(discovery, options.SeasonID, options.MaxSeasons, now)
		return selected, nil, err
	}
	completed, err := selectSeasons(discovery, "", len(discovery.Seasons), now)
	if err != nil {
		return nil, nil, err
	}
	selected := make([]provider.SeasonSummary, 0, options.MaxSeasons)
	reused := make([]string, 0)
	for _, season := range completed {
		complete, coverageErr := r.Repo.SeasonCoverageComplete(ctx, season.ID.String())
		if coverageErr != nil {
			return nil, nil, coverageErr
		}
		if complete {
			reused = append(reused, season.ID.String())
			continue
		}
		selected = append(selected, season)
		if len(selected) >= options.MaxSeasons {
			break
		}
	}
	if len(selected) == 0 && len(reused) == 0 {
		return nil, nil, errors.New("no completed seasons available for ingestion")
	}
	return selected, reused, nil
}

func countCompletedSeasons(discovery provider.SeasonDiscovery, now time.Time) int {
	count := 0
	for _, season := range discovery.Seasons {
		if !season.Current && season.EndUTC.Before(now) {
			count++
		}
	}
	return count
}

func (r *Runner) discoverSeasons(ctx context.Context) (provider.SeasonDiscovery, string, error) {
	jobID, _, attempt, err := r.Repo.EnsureJob(ctx, "season_discovery", "", 0, "season-discovery:"+r.Config.TournamentID.String(), r.Config.MaxRetries+1)
	if err != nil {
		return provider.SeasonDiscovery{}, "", err
	}
	_ = r.Repo.SetJobStatus(ctx, jobID, "running", "")
	requestKey := fmt.Sprintf("uniquetournament_seasons/%s", r.Config.TournamentID)
	response, rawPayloadID, err := r.fetchAndStore(ctx, "season_discovery", requestKey, r.Config.HistoricalBaseURL, requestKey, map[string]any{
		"tournament_id": r.Config.TournamentID.String(),
	})
	if err != nil {
		_ = r.Repo.RecordFailure(ctx, jobID, "season-discovery:"+r.Config.TournamentID.String(), attempt+1, "provider_request_failed", err.Error(), isRetryableError(err), responseStatus(response))
		_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
		return provider.SeasonDiscovery{}, rawPayloadID, err
	}
	discovery, err := provider.ParseSeasonDiscovery(response.Body)
	if err != nil {
		_ = r.Repo.InsertValidationError(ctx, jobID, rawPayloadID, "season_discovery", requestKey, "season_discovery_schema_invalid", err.Error(), `{}`)
		_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
		return provider.SeasonDiscovery{}, rawPayloadID, err
	}
	now := r.now()
	for _, season := range discovery.Seasons {
		if err := r.Repo.UpsertSeason(ctx, season, rawPayloadID, now); err != nil {
			_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
			return provider.SeasonDiscovery{}, rawPayloadID, fmt.Errorf("persist season %s: %w", season.ID, err)
		}
	}
	_ = r.Repo.SetJobStatus(ctx, jobID, "completed", "")
	return discovery, rawPayloadID, nil
}

func selectSeasons(discovery provider.SeasonDiscovery, requestedID string, maxSeasons int, now time.Time) ([]provider.SeasonSummary, error) {
	if strings.TrimSpace(requestedID) != "" {
		for _, season := range discovery.Seasons {
			if season.ID.String() == requestedID {
				if season.Current || !season.EndUTC.Before(now) {
					return nil, fmt.Errorf("season %s is not a completed season", requestedID)
				}
				return []provider.SeasonSummary{season}, nil
			}
		}
		return nil, fmt.Errorf("season %s was not present in the discovery response", requestedID)
	}
	completed := make([]provider.SeasonSummary, 0)
	for _, season := range discovery.Seasons {
		if !season.Current && season.EndUTC.Before(now) {
			completed = append(completed, season)
		}
	}
	sort.SliceStable(completed, func(i, j int) bool {
		return completed[i].StartUTC.After(completed[j].StartUTC)
	})
	if len(completed) == 0 {
		return nil, errors.New("season discovery returned no completed seasons")
	}
	if maxSeasons > len(completed) {
		maxSeasons = len(completed)
	}
	return completed[:maxSeasons], nil
}

func (r *Runner) IngestSeason(ctx context.Context, season provider.SeasonSummary, discoveryRawPayloadID string) (SeasonReport, error) {
	report := SeasonReport{SeasonID: season.ID.String()}
	jobKey := "season-backfill:" + season.ID.String()
	jobID, jobStatus, attempt, err := r.Repo.EnsureJob(ctx, "season_backfill", season.ID.String(), 0, jobKey, r.Config.MaxRetries+1)
	if err != nil {
		return report, err
	}
	if jobStatus == "completed" || jobStatus == "completed_with_errors" {
		coverage, coverageErr := r.Repo.Coverage(ctx, season.ID.String())
		if coverageErr == nil {
			report.Coverage = coverage
		}
		return report, nil
	}
	if err := r.Repo.SetJobStatus(ctx, jobID, "running", ""); err != nil {
		return report, err
	}

	fixtureRequestKey := fmt.Sprintf("stats_season_fixtures2/%s", season.ID)
	fixtureResponse, fixtureRawPayloadID, err := r.fetchAndStore(ctx, "season_fixtures", fixtureRequestKey, r.Config.HistoricalBaseURL, fixtureRequestKey, map[string]any{
		"season_id": season.ID.String(),
	})
	if err != nil {
		r.reportFailure(ctx, jobID, "season-fixtures:"+season.ID.String(), attempt+1, err)
		_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
		return report, err
	}
	fixturesPayload, err := provider.ParseFixtures(fixtureResponse.Body)
	if err != nil {
		r.recordValidation(ctx, jobID, fixtureRawPayloadID, "fixture_payload", season.ID.String(), "fixture_schema_invalid", err)
		_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
		return report, err
	}
	if fixturesPayload.Season.ID != season.ID || fixturesPayload.Season.TournamentID != season.TournamentID {
		err := fmt.Errorf("fixture season identity %s/%s does not match discovery %s/%s", fixturesPayload.Season.ID, fixturesPayload.Season.TournamentID, season.ID, season.TournamentID)
		r.recordValidation(ctx, jobID, fixtureRawPayloadID, "fixture_payload", season.ID.String(), "fixture_season_identity_mismatch", err)
		_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
		return report, err
	}
	if err := r.persistFixtures(ctx, fixturesPayload, fixtureRawPayloadID, season, jobID, &report); err != nil {
		_ = r.Repo.SetJobStatus(ctx, jobID, "failed", err.Error())
		return report, err
	}

	for round := 1; round <= 30; round++ {
		roundReport, err := r.ingestOddsRound(ctx, season, round, fixturesPayload.Matches, jobID)
		report.OddsRoundsAttempted++
		report.AcceptedOutcomes += roundReport.AcceptedOutcomes
		report.QuarantinedRecords += roundReport.QuarantinedRecords
		report.JoinFailures += roundReport.JoinFailures
		report.Errors = append(report.Errors, roundReport.Errors...)
		if err != nil {
			report.OddsRoundsWithErrors++
			continue
		}
		if roundReport.HasErrors {
			report.OddsRoundsWithErrors++
		} else {
			report.OddsRoundsCompleted++
		}
	}
	coverage, coverageErr := r.Repo.Coverage(ctx, season.ID.String())
	if coverageErr != nil {
		return report, coverageErr
	}
	report.Coverage = coverage
	finalStatus := "completed"
	if len(report.Errors) > 0 || report.FixtureValidationErr > 0 || report.OddsRoundsWithErrors > 0 {
		finalStatus = "completed_with_errors"
	}
	if err := r.Repo.SetJobStatus(ctx, jobID, finalStatus, strings.Join(report.Errors, "; ")); err != nil {
		return report, err
	}
	_ = discoveryRawPayloadID
	return report, nil
}

func (r *Runner) persistFixtures(ctx context.Context, payload provider.FixturePayload, rawPayloadID string, season provider.SeasonSummary, jobID string, report *SeasonReport) error {
	now := r.now()
	if err := r.Repo.UpsertSeason(ctx, season, rawPayloadID, now); err != nil {
		return err
	}
	for _, fixture := range payload.Matches {
		if err := r.Repo.UpsertTeam(ctx, fixture.Home, rawPayloadID, now); err != nil {
			return fmt.Errorf("upsert home team %s: %w", fixture.Home.UID, err)
		}
		if err := r.Repo.UpsertTeam(ctx, fixture.Away, rawPayloadID, now); err != nil {
			return fmt.Errorf("upsert away team %s: %w", fixture.Away.UID, err)
		}
		if err := r.Repo.UpsertFixture(ctx, fixture, rawPayloadID, now, r.Config.SettlementConfirmation); err != nil {
			return fmt.Errorf("upsert fixture %s: %w", fixture.MatchID, err)
		}
	}
	report.FixtureCount = len(payload.Matches)
	rounds := make(map[provider.RoundNumber]int)
	for _, fixture := range payload.Matches {
		rounds[fixture.Round]++
	}
	report.FixtureRoundCount = len(rounds)
	if len(payload.Matches) != 240 || len(rounds) != 30 {
		report.FixtureValidationErr++
		r.recordValidation(ctx, jobID, rawPayloadID, "fixture_coverage", season.ID.String(), "fixture_coverage_incomplete", fmt.Errorf("got %d fixtures across %d rounds, want 240 across 30 rounds", len(payload.Matches), len(rounds)))
	}
	for round := provider.RoundNumber(1); round <= 30; round++ {
		if rounds[round] != 8 {
			report.FixtureValidationErr++
			r.recordValidation(ctx, jobID, rawPayloadID, "fixture_round", fmt.Sprintf("%s/%d", season.ID, round), "fixture_round_count_mismatch", fmt.Errorf("got %d fixtures, want 8", rounds[round]))
		}
	}
	return nil
}

type oddsRoundReport struct {
	AcceptedOutcomes   int
	QuarantinedRecords int
	JoinFailures       int
	HasErrors          bool
	Errors             []string
}

func (r *Runner) ingestOddsRound(ctx context.Context, season provider.SeasonSummary, round int, fixtures []provider.Fixture, jobID string) (oddsRoundReport, error) {
	result := oddsRoundReport{}
	checkpointKey := fmt.Sprintf("odds:%s:%d", season.ID, round)
	checkpoint, err := r.getCheckpoint(ctx, checkpointKey)
	if err != nil {
		return result, err
	}
	if checkpoint.State == "completed" || checkpoint.State == "completed_with_errors" {
		return result, nil
	}
	checkpoint.Key = checkpointKey
	checkpoint.State = "running"
	checkpoint.Attempt++
	checkpoint.RequestCount++
	checkpoint.ExpectedCount = 8
	if err := r.Repo.UpsertCheckpoint(ctx, checkpoint, "season_odds", season.ID.String(), round); err != nil {
		return result, err
	}
	requestKey := fmt.Sprintf("stats_season_odds2/vf:season:%s/%d", season.ID, round)
	response, rawPayloadID, err := r.fetchAndStore(ctx, "season_odds", requestKey, r.Config.LiveBaseURL, requestKey, map[string]any{
		"season_id": season.ID.String(),
		"round":     round,
	})
	if err != nil {
		result.HasErrors = true
		result.Errors = append(result.Errors, fmt.Sprintf("round %d request: %v", round, err))
		checkpoint.State = "failed"
		checkpoint.LastError = err.Error()
		_ = r.Repo.UpsertCheckpoint(ctx, checkpoint, "season_odds", season.ID.String(), round)
		_ = r.Repo.RecordFailure(ctx, jobID, checkpointKey, checkpoint.Attempt, "odds_request_failed", err.Error(), isRetryableError(err), responseStatus(response))
		return result, err
	}
	snapshot, err := provider.ParseOdds(response.Body)
	if err != nil {
		result.HasErrors = true
		result.Errors = append(result.Errors, fmt.Sprintf("round %d schema: %v", round, err))
		r.recordValidation(ctx, jobID, rawPayloadID, "odds_payload", requestKey, "odds_schema_invalid", err)
		checkpoint.State = "failed"
		checkpoint.LastError = err.Error()
		_ = r.Repo.UpsertCheckpoint(ctx, checkpoint, "season_odds", season.ID.String(), round)
		return result, err
	}
	for _, issue := range snapshot.ValidationIssues {
		result.HasErrors = true
		result.Errors = append(result.Errors, fmt.Sprintf("round %d invalid odds %s/%d/%s: %s", round, issue.MatchID, issue.MarketID, issue.OutcomeID, issue.Reason))
		fragment, _ := json.Marshal(issue)
		_ = r.Repo.InsertValidationError(ctx, jobID, rawPayloadID, "odds_outcome", fmt.Sprintf("%s/%d/%s", issue.MatchID, issue.MarketID, issue.OutcomeID), "invalid_decimal_odds", issue.Reason, string(fragment))
	}
	if len(snapshot.Records) != 8 {
		result.HasErrors = true
		result.Errors = append(result.Errors, fmt.Sprintf("round %d returned %d odds records, want 8", round, len(snapshot.Records)))
		_ = r.Repo.InsertValidationError(ctx, jobID, rawPayloadID, "odds_round", requestKey, "odds_match_count_mismatch", fmt.Sprintf("returned %d records, want 8", len(snapshot.Records)), `{}`)
	}
	roundFixtures := make(map[provider.MatchID]provider.Fixture, len(fixtures)/30+1)
	for _, fixture := range fixtures {
		if fixture.Round.Int() == round {
			roundFixtures[fixture.MatchID] = fixture
		}
	}
	if len(roundFixtures) != 8 {
		result.HasErrors = true
		result.Errors = append(result.Errors, fmt.Sprintf("round %d has %d fixture records, want 8", round, len(roundFixtures)))
	}
	provenanceBytes, _ := json.Marshal(map[string]any{
		"endpoint":                   requestKey,
		"observation_kind":           r.Config.HistoricalObservationKind,
		"point_in_time_verified":     false,
		"returned_after_settlement":  true,
		"source_timestamp_semantics": "provider timestamp retained; publication time unverified",
	})
	persisted, err := r.Repo.UpsertOddsSnapshot(ctx, snapshot, roundFixtures, rawPayloadID, response.FetchedAt, r.Config.HistoricalObservationKind, string(provenanceBytes), jobID, checkpointKey)
	if err != nil {
		return result, err
	}
	result.AcceptedOutcomes += persisted.Accepted
	result.QuarantinedRecords += persisted.Quarantined
	result.JoinFailures += persisted.JoinFailure
	if persisted.JoinFailure > 0 {
		result.HasErrors = true
		result.Errors = append(result.Errors, fmt.Sprintf("round %d has %d odds-to-fixture join failures", round, persisted.JoinFailure))
	}
	checkpoint.AcceptedCount = persisted.Accepted
	checkpoint.QuarantinedCount = persisted.Quarantined
	checkpoint.LastRawPayloadID = rawPayloadID
	if result.HasErrors {
		checkpoint.State = "completed_with_errors"
	} else {
		checkpoint.State = "completed"
	}
	if err := r.Repo.UpsertCheckpoint(ctx, checkpoint, "season_odds", season.ID.String(), round); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Runner) fetchAndStore(ctx context.Context, endpointFamily, requestKey, baseURL, relativePath string, params map[string]any) (*Response, string, error) {
	validators := CacheValidators{}
	if stored, err := r.Repo.LatestRawPayload(ctx, endpointFamily, requestKey); err == nil {
		validators = CacheValidators{ETag: stored.ETag, LastModified: stored.LastModified}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}
	response, err := r.Client.Get(ctx, baseURL, relativePath, validators)
	if response == nil {
		return nil, "", err
	}
	cacheReused := false
	if response.StatusCode == 304 {
		stored, cacheErr := r.Repo.LatestRawPayload(ctx, endpointFamily, requestKey)
		if cacheErr != nil {
			return response, "", fmt.Errorf("provider returned 304 but no cached payload is available: %w", cacheErr)
		}
		response.Body = stored.Payload
		response.StatusCode = httpStatusOK
		if response.ETag == "" {
			response.ETag = stored.ETag
		}
		if response.LastModified == "" {
			response.LastModified = stored.LastModified
		}
		response.CacheReused = true
		cacheReused = true
	}
	if len(response.Body) > 0 {
		paramBytes, _ := json.Marshal(params)
		provenanceBytes, _ := json.Marshal(map[string]any{
			"cache_reused": cacheReused,
			"http_status":  response.StatusCode,
		})
		rawPayloadID, saveErr := r.Repo.SaveRawPayload(ctx, RawPayloadInput{
			EndpointFamily: endpointFamily,
			RequestKey:     requestKey,
			RequestURL:     response.URL,
			RequestParams:  string(paramBytes),
			Response:       response,
			Payload:        response.Body,
			Provenance:     string(provenanceBytes),
		})
		if saveErr != nil {
			return response, "", saveErr
		}
		if err != nil {
			return response, rawPayloadID, err
		}
		if !json.Valid(response.Body) {
			return response, rawPayloadID, fmt.Errorf("provider response %s contains malformed JSON", response.URL)
		}
		return response, rawPayloadID, nil
	}
	if err != nil {
		return response, "", err
	}
	return response, "", fmt.Errorf("provider response %s returned non-JSON or empty body", response.URL)
}

// FetchAndStore exposes the same cache-aware, raw-retaining request path used
// by historical ingestion to the live workers.  Keeping one implementation
// prevents live polling from bypassing conditional requests or provenance.
func (r *Runner) FetchAndStore(ctx context.Context, endpointFamily, requestKey, baseURL, relativePath string, params map[string]any) (*Response, string, error) {
	return r.fetchAndStore(ctx, endpointFamily, requestKey, baseURL, relativePath, params)
}

const httpStatusOK = 200

func (r *Runner) getCheckpoint(ctx context.Context, key string) (Checkpoint, error) {
	checkpoint, err := r.Repo.GetCheckpoint(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{Key: key, State: "pending"}, nil
	}
	return checkpoint, err
}

func (r *Runner) recordValidation(ctx context.Context, jobID, rawPayloadID, entityType, entityKey, code string, err error) {
	if r.Repo == nil {
		return
	}
	_ = r.Repo.InsertValidationError(ctx, jobID, rawPayloadID, entityType, entityKey, code, err.Error(), `{}`)
}

func (r *Runner) reportFailure(ctx context.Context, jobID, checkpointKey string, attempt int, err error) {
	if r.Repo == nil {
		return
	}
	_ = r.Repo.RecordFailure(ctx, jobID, checkpointKey, attempt, "ingestion_failed", err.Error(), isRetryableError(err), responseStatus(nil))
}

func responseStatus(response *Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}

func isRetryableError(err error) bool {
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return retryableStatus(statusErr.StatusCode)
	}
	return true
}

func ValidateFixtureCoverage(fixtures []provider.Fixture) []string {
	errors := make([]string, 0)
	if len(fixtures) != 240 {
		errors = append(errors, fmt.Sprintf("fixture count=%d want 240", len(fixtures)))
	}
	rounds := make(map[provider.RoundNumber]int)
	ids := make(map[provider.MatchID]struct{})
	for _, fixture := range fixtures {
		rounds[fixture.Round]++
		if _, exists := ids[fixture.MatchID]; exists {
			errors = append(errors, fmt.Sprintf("duplicate fixture ID %s", fixture.MatchID))
		}
		ids[fixture.MatchID] = struct{}{}
	}
	if len(ids) != len(fixtures) {
		errors = append(errors, fmt.Sprintf("distinct fixture IDs=%d want %d", len(ids), len(fixtures)))
	}
	if len(rounds) != 30 {
		errors = append(errors, fmt.Sprintf("round count=%d want 30", len(rounds)))
	}
	for round := provider.RoundNumber(1); round <= 30; round++ {
		if rounds[round] != 8 {
			errors = append(errors, fmt.Sprintf("round %d fixture count=%d want 8", round, rounds[round]))
		}
	}
	return errors
}

func ValidateOddsJoins(fixtures []provider.Fixture, odds provider.OddsSnapshot) []string {
	fixtureIDs := make(map[provider.MatchID]provider.Fixture, len(fixtures))
	for _, fixture := range fixtures {
		fixtureIDs[fixture.MatchID] = fixture
	}
	errors := make([]string, 0)
	for _, record := range odds.Records {
		fixture, exists := fixtureIDs[record.MatchID]
		if !exists {
			errors = append(errors, fmt.Sprintf("odds match %s has no fixture", record.MatchID))
			continue
		}
		if fixture.Home.UID != record.HomeTeamUID || fixture.Away.UID != record.AwayTeamUID {
			errors = append(errors, fmt.Sprintf("odds match %s team IDs do not match fixture", record.MatchID))
		}
	}
	return errors
}
