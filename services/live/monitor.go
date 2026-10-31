package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"bet9ja-vfl/services/autobet"
	"bet9ja-vfl/services/ingest"
	"bet9ja-vfl/services/provider"
	"bet9ja-vfl/services/settlement"
)

// PollConfig follows the conservative defaults in specification section 6.2.
// The shared ingest.ProviderClient budget remains the final request gate.
type PollConfig struct {
	PollInterval            time.Duration
	SeasonDiscoveryInterval time.Duration
	FixtureInterval         time.Duration
	UpcomingOddsInterval    time.Duration
	FutureOddsInterval      time.Duration
	RecentlyCompleted       time.Duration
	ImminentWindow          time.Duration
	PredictionSafetyBuffer  time.Duration
	FutureRounds            int
	LeaseTTL                time.Duration
	WorkerName              string
	OwnerID                 string
	CircuitFailureThreshold int
	CircuitCooldown         time.Duration
}

func DefaultPollConfig() PollConfig {
	return PollConfig{
		PollInterval:            5 * time.Second,
		SeasonDiscoveryInterval: 5 * time.Minute,
		FixtureInterval:         2 * time.Minute,
		UpcomingOddsInterval:    90 * time.Second,
		FutureOddsInterval:      10 * time.Minute,
		RecentlyCompleted:       2 * time.Minute,
		ImminentWindow:          5 * time.Minute,
		PredictionSafetyBuffer:  30 * time.Second,
		FutureRounds:            2,
		LeaseTTL:                4 * time.Minute,
		WorkerName:              "live-monitor",
		OwnerID:                 "local-live-monitor",
		CircuitFailureThreshold: 3,
		CircuitCooldown:         5 * time.Minute,
	}
}

func (c PollConfig) normalize() PollConfig {
	defaults := DefaultPollConfig()
	if c.PollInterval <= 0 {
		c.PollInterval = defaults.PollInterval
	}
	if c.SeasonDiscoveryInterval <= 0 {
		c.SeasonDiscoveryInterval = defaults.SeasonDiscoveryInterval
	}
	if c.FixtureInterval <= 0 {
		c.FixtureInterval = defaults.FixtureInterval
	}
	if c.UpcomingOddsInterval <= 0 {
		c.UpcomingOddsInterval = defaults.UpcomingOddsInterval
	}
	if c.FutureOddsInterval <= 0 {
		c.FutureOddsInterval = defaults.FutureOddsInterval
	}
	if c.RecentlyCompleted <= 0 {
		c.RecentlyCompleted = defaults.RecentlyCompleted
	}
	if c.ImminentWindow <= 0 {
		c.ImminentWindow = defaults.ImminentWindow
	}
	if c.PredictionSafetyBuffer <= 0 {
		c.PredictionSafetyBuffer = defaults.PredictionSafetyBuffer
	}
	if c.FutureRounds <= 0 {
		c.FutureRounds = defaults.FutureRounds
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = defaults.LeaseTTL
	}
	if strings.TrimSpace(c.WorkerName) == "" {
		c.WorkerName = defaults.WorkerName
	}
	if strings.TrimSpace(c.OwnerID) == "" {
		c.OwnerID = defaults.OwnerID
	}
	if c.CircuitFailureThreshold <= 0 {
		c.CircuitFailureThreshold = defaults.CircuitFailureThreshold
	}
	if c.CircuitCooldown <= 0 {
		c.CircuitCooldown = defaults.CircuitCooldown
	}
	return c
}

// PollPlan is pure scheduling logic so the fixed monitor cadence can be
// verified without a provider. The other fields remain on the type for
// compatibility with existing callers, but the background monitor uses one
// fixed interval regardless of kickoff timing or provider errors.
type PollPlan struct {
	Now               time.Time
	NextKickoff       time.Time
	RecentlyCompleted bool
	ProviderBackoff   time.Duration
	Config            PollConfig
}

func (p PollPlan) NextDelay() time.Duration {
	cfg := p.Config.normalize()
	if cfg.PollInterval < time.Second {
		return time.Second
	}
	return cfg.PollInterval
}

type CircuitBreaker struct {
	mu        sync.Mutex
	failures  int
	threshold int
	openedAt  time.Time
	cooldown  time.Duration
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	return &CircuitBreaker{threshold: threshold, cooldown: cooldown}
}

func (b *CircuitBreaker) Allow(now time.Time) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return true
	}
	if now.Sub(b.openedAt) >= b.cooldown {
		return true
	}
	return false
}

func (b *CircuitBreaker) Success() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.failures = 0
	b.openedAt = time.Time{}
	b.mu.Unlock()
}

func (b *CircuitBreaker) Failure(now time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.failures++
	if b.failures >= b.threshold {
		b.openedAt = now
	}
	b.mu.Unlock()
}

type Monitor struct {
	Config         PollConfig
	ProviderConfig ingest.Config
	Client         *ingest.ProviderClient
	Repo           *ingest.Repository
	Runner         *ingest.Runner
	Now            func() time.Time
	Circuit        *CircuitBreaker
}

func NewMonitor(cfg PollConfig, client *ingest.ProviderClient, repo *ingest.Repository) *Monitor {
	return NewMonitorWithProviderConfig(cfg, ingest.DefaultConfig(), client, repo)
}

func NewMonitorWithProviderConfig(cfg PollConfig, providerConfig ingest.Config, client *ingest.ProviderClient, repo *ingest.Repository) *Monitor {
	cfg = cfg.normalize()
	return &Monitor{
		Config:         cfg,
		ProviderConfig: providerConfig,
		Client:         client,
		Repo:           repo,
		Runner:         ingest.NewRunner(providerConfig, client, repo),
		Now:            time.Now,
		Circuit:        NewCircuitBreaker(cfg.CircuitFailureThreshold, cfg.CircuitCooldown),
	}
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

type PollReport struct {
	SeasonID          string    `json:"season_id"`
	FixtureCount      int       `json:"fixture_count"`
	UpcomingRounds    []int     `json:"upcoming_rounds"`
	OddsRounds        []int     `json:"odds_rounds"`
	OddsChanges       int       `json:"odds_changes"`
	FirstKickoff      time.Time `json:"first_kickoff"`
	ObservedAt        time.Time `json:"observed_at"`
	CacheObservations int       `json:"cache_observations"`
	Warnings          []string  `json:"warnings,omitempty"`
}

// PollOnce executes one leased monitor cycle. It is intentionally independent
// of HTTP handlers and can be called by a background worker or a verification
// command.
func (m *Monitor) PollOnce(ctx context.Context) (PollReport, error) {
	if m == nil || m.Client == nil || m.Repo == nil || m.Runner == nil {
		return PollReport{}, errors.New("live monitor is not initialized")
	}
	now := m.now()
	if !m.Circuit.Allow(now) {
		return PollReport{}, errors.New("live provider circuit is open")
	}
	leaseToken, acquired, err := m.Repo.AcquireLease(ctx, "live-monitor", m.Config.OwnerID, m.Config.LeaseTTL)
	if err != nil {
		return PollReport{}, err
	}
	if !acquired {
		return PollReport{}, errors.New("live monitor lease is held by another worker")
	}
	defer func() { _ = m.Repo.ReleaseLease(context.Background(), "live-monitor", m.Config.OwnerID, leaseToken) }()
	executionID, err := m.Repo.StartJobExecution(ctx, "live-monitor", m.Config.WorkerName, 1, `{"scope":"season-fixtures-and-upcoming-odds"}`)
	if err != nil {
		return PollReport{}, err
	}
	report, pollErr := m.pollOnceLeased(ctx, now)
	if pollErr != nil {
		m.Circuit.Failure(now)
		flag := true
		_ = m.Repo.FinishJobExecution(ctx, executionID, "failed", "live_poll_failed", pollErr.Error(), &flag, 0, report.CacheObservations, `{"retriable":true}`)
		return report, pollErr
	}
	m.Circuit.Success()
	_ = m.Repo.FinishJobExecution(ctx, executionID, "succeeded", "", "", nil, report.FixtureCount, report.CacheObservations, `{}`)
	return report, nil
}

func (m *Monitor) pollOnceLeased(ctx context.Context, now time.Time) (PollReport, error) {
	cfg := m.ProviderConfig
	if cfg.LiveBaseURL == "" {
		cfg = ingest.DefaultConfig()
	}
	// Runner uses the same ProviderClient budget and repository cache path as
	// historical ingestion. Its config is replaced below for environment/base
	// URL compatibility when callers constructed a custom client.
	m.Runner.Config = cfg
	requestKey := fmt.Sprintf("uniquetournament_seasons/%s", cfg.TournamentID)
	response, rawID, err := m.Runner.FetchAndStore(ctx, "live_season_discovery", requestKey, cfg.LiveBaseURL, requestKey, map[string]any{"tournament_id": cfg.TournamentID.String()})
	if err != nil {
		return PollReport{}, err
	}
	if response == nil || strings.TrimSpace(rawID) == "" {
		return PollReport{}, errors.New("season discovery did not produce a raw payload")
	}
	if err := m.Repo.SaveObservation(ctx, "live_season_discovery", requestKey, response, rawID, response.CacheReused, "", 0, "accepted", `{}`); err != nil {
		return PollReport{}, err
	}
	cacheObservations := 0
	if response.CacheReused {
		cacheObservations++
	}
	discovery, err := provider.ParseSeasonDiscovery(response.Body)
	if err != nil {
		_ = m.Repo.SaveObservation(ctx, "live_season_discovery", requestKey, response, rawID, false, "", 0, "invalid", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return PollReport{}, err
	}
	current, err := currentSeason(discovery, now)
	if err != nil {
		return PollReport{}, err
	}
	for _, season := range discovery.Seasons {
		if err := m.Repo.UpsertSeason(ctx, season, rawID, now); err != nil {
			return PollReport{}, err
		}
	}
	if err := m.Repo.ReconcileCurrentSeasonFlags(ctx, current.ID.String()); err != nil {
		return PollReport{}, err
	}
	fixtureKey := fmt.Sprintf("stats_season_fixtures2/%s", current.ID)
	fixtureResponse, fixtureRawID, err := m.Runner.FetchAndStore(ctx, "live_season_fixtures", fixtureKey, cfg.LiveBaseURL, fixtureKey, map[string]any{"season_id": current.ID.String()})
	if err != nil {
		return PollReport{SeasonID: current.ID.String(), ObservedAt: now}, err
	}
	if err := m.Repo.SaveObservation(ctx, "live_season_fixtures", fixtureKey, fixtureResponse, fixtureRawID, fixtureResponse.CacheReused, current.ID.String(), 0, "accepted", `{}`); err != nil {
		return PollReport{}, err
	}
	if fixtureResponse.CacheReused {
		cacheObservations++
	}
	fixturesPayload, err := provider.ParseFixtures(fixtureResponse.Body)
	if err != nil {
		_ = m.Repo.SaveObservation(ctx, "live_season_fixtures", fixtureKey, fixtureResponse, fixtureRawID, false, current.ID.String(), 0, "invalid", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return PollReport{}, err
	}
	if fixturesPayload.Season.ID != current.ID || fixturesPayload.Season.TournamentID != current.TournamentID {
		return PollReport{}, fmt.Errorf("live fixture identity %s/%s does not match current season %s/%s", fixturesPayload.Season.ID, fixturesPayload.Season.TournamentID, current.ID, current.TournamentID)
	}
	for _, fixture := range fixturesPayload.Matches {
		if err := m.Repo.UpsertTeam(ctx, fixture.Home, fixtureRawID, now); err != nil {
			return PollReport{}, err
		}
		if err := m.Repo.UpsertTeam(ctx, fixture.Away, fixtureRawID, now); err != nil {
			return PollReport{}, err
		}
		if err := m.Repo.UpsertFixture(ctx, fixture, fixtureRawID, now, 120*time.Second); err != nil {
			return PollReport{}, err
		}
	}
	resultWarnings := make([]string, 0, 1)
	if err := m.refreshResultFixtures(ctx, cfg, current, now); err != nil {
		// The result feed is supplementary. Keep the primary fixture/odds
		// polling path healthy if it is temporarily unavailable.
		resultWarnings = append(resultWarnings, fmt.Sprintf("results feed: %v", err))
	}
	if err := m.settleObservedResults(ctx); err != nil {
		return PollReport{}, err
	}
	upcoming, kickoff := upcomingRounds(fixturesPayload.Matches, now)
	if len(upcoming) == 0 {
		return PollReport{SeasonID: current.ID.String(), FixtureCount: len(fixturesPayload.Matches), FirstKickoff: kickoff, ObservedAt: now, CacheObservations: cacheObservations, Warnings: resultWarnings}, nil
	}
	if len(upcoming) > m.Config.FutureRounds {
		upcoming = upcoming[:m.Config.FutureRounds]
	}
	report := PollReport{SeasonID: current.ID.String(), FixtureCount: len(fixturesPayload.Matches), UpcomingRounds: upcoming, FirstKickoff: kickoff, ObservedAt: now, CacheObservations: cacheObservations, Warnings: resultWarnings}
	for _, round := range upcoming {
		oddsKey := fmt.Sprintf("stats_season_odds2/vf:season:%s/%d", current.ID, round)
		oddsResponse, oddsRawID, oddsErr := m.Runner.FetchAndStore(ctx, "live_season_odds", oddsKey, cfg.LiveBaseURL, oddsKey, map[string]any{"season_id": current.ID.String(), "round": round})
		if oddsResponse != nil && oddsRawID != "" {
			if oddsResponse.CacheReused {
				report.CacheObservations++
			}
			_ = m.Repo.SaveObservation(ctx, "live_season_odds", oddsKey, oddsResponse, oddsRawID, oddsResponse.CacheReused, current.ID.String(), round, "accepted", `{}`)
		}
		if oddsErr != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("round %d odds: %v", round, oddsErr))
			continue
		}
		snapshot, parseErr := provider.ParseOdds(oddsResponse.Body)
		if parseErr != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("round %d odds schema: %v", round, parseErr))
			if oddsResponse != nil && oddsRawID != "" {
				_ = m.Repo.SaveObservation(ctx, "live_season_odds", oddsKey, oddsResponse, oddsRawID, false, current.ID.String(), round, "invalid", fmt.Sprintf(`{"error":%q}`, parseErr.Error()))
			}
			continue
		}
		roundFixtures := make(map[provider.MatchID]provider.Fixture)
		for _, fixture := range fixturesPayload.Matches {
			if fixture.Round.Int() == round {
				roundFixtures[fixture.MatchID] = fixture
			}
		}
		provenance, _ := json.Marshal(map[string]any{
			"observation_kind":           "live_observation",
			"fetched_at":                 oddsResponse.FetchedAt.UTC().Format(time.RFC3339Nano),
			"point_in_time_verified":     "true only when local fetched_at precedes fixture kickoff",
			"source_timestamp_semantics": "provider timestamp retained; local observation is the evidence time",
		})
		persisted, persistErr := m.Repo.UpsertLiveOddsSnapshot(ctx, snapshot, roundFixtures, oddsRawID, oddsResponse.FetchedAt, string(provenance), "", "")
		if persistErr != nil {
			return report, persistErr
		}
		report.OddsRounds = append(report.OddsRounds, round)
		for matchID := range roundFixtures {
			changes, changeErr := m.Repo.DetectAndRecordOddsChanges(ctx, int64(matchID), oddsResponse.FetchedAt, oddsRawID)
			if changeErr != nil {
				return report, changeErr
			}
			report.OddsChanges += changes
		}
		if persisted.JoinFailure > 0 || persisted.Quarantined > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("round %d odds accepted=%d quarantined=%d joins=%d", round, persisted.Accepted, persisted.Quarantined, persisted.JoinFailure))
		}
	}
	for _, round := range upcoming {
		matches := fixturesForRound(fixturesPayload.Matches, round)
		if len(matches) == 0 {
			continue
		}
		first := matches[0].ScheduledAt
		for _, fixture := range matches[1:] {
			if fixture.ScheduledAt.Before(first) {
				first = fixture.ScheduledAt
			}
		}
		state := "provisional"
		if now.After(first) || now.Equal(first) {
			state = "missed_cutoff"
		}
		_ = m.Repo.UpsertRoundState(ctx, current.ID.String(), round, first, first.Add(-m.Config.PredictionSafetyBuffer), state, fmt.Sprintf(`{"season_observed":%q,"odds_observed":true}`, now.Format(time.RFC3339Nano)))
	}
	return report, nil
}

func (m *Monitor) refreshResultFixtures(ctx context.Context, cfg ingest.Config, current provider.SeasonSummary, now time.Time) error {
	resultsBaseURL := fixtureResultsBaseURL(cfg)
	fixtureKey := fmt.Sprintf("stats_season_fixtures2/%s", current.ID)
	requestKey := "results:" + fixtureKey
	response, rawID, err := m.Runner.FetchAndStore(ctx, "live_season_results", requestKey, resultsBaseURL, fixtureKey, map[string]any{"season_id": current.ID.String()})
	if err != nil {
		return err
	}
	if response == nil || strings.TrimSpace(rawID) == "" {
		return errors.New("season results did not produce a raw payload")
	}
	if err := m.Repo.SaveObservation(ctx, "live_season_results", requestKey, response, rawID, response.CacheReused, current.ID.String(), 0, "accepted", `{}`); err != nil {
		return err
	}
	payload, err := provider.ParseFixtures(response.Body)
	if err != nil {
		_ = m.Repo.SaveObservation(ctx, "live_season_results", requestKey, response, rawID, false, current.ID.String(), 0, "invalid", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return err
	}
	if payload.Season.ID != current.ID || payload.Season.TournamentID != current.TournamentID {
		return fmt.Errorf("results identity %s/%s does not match current season %s/%s", payload.Season.ID, payload.Season.TournamentID, current.ID, current.TournamentID)
	}
	for _, fixture := range payload.Matches {
		if err := m.Repo.UpsertFixtureResults(ctx, fixture, rawID, now, 120*time.Second); err != nil {
			return fmt.Errorf("update result for match %s: %w", fixture.MatchID, err)
		}
	}
	return nil
}

func fixtureResultsBaseURL(cfg ingest.Config) string {
	if value := strings.TrimSpace(cfg.ResultsBaseURL); value != "" {
		return value
	}
	return ingest.DefaultResultsBaseURL
}

func currentSeason(discovery provider.SeasonDiscovery, now time.Time) (provider.SeasonSummary, error) {
	var flagged *provider.SeasonSummary
	for index := range discovery.Seasons {
		season := discovery.Seasons[index]
		if !season.Current {
			continue
		}
		if flagged != nil {
			return provider.SeasonSummary{}, errors.New("season discovery contains multiple current seasons")
		}
		flagged = &season
	}

	// The provider's current flag is authoritative while it describes an
	// active season. During a season transition, however, the catalog can
	// briefly retain the previous flag while already returning the next
	// season with a valid active date window. Use that explicit provider date
	// range as a bounded transition fallback; do not infer a season from an ID
	// or from an arbitrary ordering of the catalog.
	active := make([]provider.SeasonSummary, 0, 1)
	for _, season := range discovery.Seasons {
		if !season.StartUTC.IsZero() && !season.EndUTC.IsZero() &&
			!now.Before(season.StartUTC) && now.Before(season.EndUTC) {
			active = append(active, season)
		}
	}
	if flagged != nil && seasonContains(flagged.StartUTC, flagged.EndUTC, now) {
		return *flagged, nil
	}
	if len(active) == 1 {
		return active[0], nil
	}
	if len(active) > 1 {
		return provider.SeasonSummary{}, errors.New("season discovery contains multiple date-active seasons")
	}
	if flagged != nil {
		// Preserve the provider-selected season across a gap between seasons;
		// the next poll can apply the date-window fallback once it starts.
		return *flagged, nil
	}
	return provider.SeasonSummary{}, errors.New("season discovery contains no current or date-active season")
}

func seasonContains(start, end, now time.Time) bool {
	return !start.IsZero() && !end.IsZero() && !now.Before(start) && now.Before(end)
}

func upcomingRounds(fixtures []provider.Fixture, now time.Time) ([]int, time.Time) {
	byRound := make(map[int][]provider.Fixture)
	for _, fixture := range fixtures {
		// A provider may leave the score empty for an in-progress or otherwise
		// unresolved fixture. Such a fixture must not block later rounds from
		// being considered upcoming: fixture persistence above still records it
		// for settlement/reconciliation, while prediction and odds selection are
		// limited to rounds whose scheduled kickoff has not passed.
		if !fixture.ScheduledAt.After(now) {
			continue
		}
		status := fixture.StatusAsOf(now, 120*time.Second)
		if status == provider.MatchStatusCompleted || status == provider.MatchStatusCanceled {
			continue
		}
		byRound[fixture.Round.Int()] = append(byRound[fixture.Round.Int()], fixture)
	}
	type roundKickoff struct {
		round   int
		kickoff time.Time
	}
	ordered := make([]roundKickoff, 0, len(byRound))
	for round, matches := range byRound {
		if len(matches) == 0 {
			continue
		}
		kickoff := matches[0].ScheduledAt
		for _, fixture := range matches[1:] {
			if fixture.ScheduledAt.Before(kickoff) {
				kickoff = fixture.ScheduledAt
			}
		}
		ordered = append(ordered, roundKickoff{round: round, kickoff: kickoff})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].kickoff.Before(ordered[j].kickoff) })
	rounds := make([]int, 0, len(ordered))
	first := time.Time{}
	for _, value := range ordered {
		rounds = append(rounds, value.round)
		if first.IsZero() || value.kickoff.Before(first) {
			first = value.kickoff
		}
	}
	return rounds, first
}

func fixturesForRound(fixtures []provider.Fixture, round int) []provider.Fixture {
	result := make([]provider.Fixture, 0, 8)
	for _, fixture := range fixtures {
		if fixture.Round.Int() == round {
			result = append(result, fixture)
		}
	}
	return result
}

func (m *Monitor) settleObservedResults(ctx context.Context) error {
	if m == nil || m.Repo == nil || m.Repo.DB == nil {
		return errors.New("live monitor repository database is not initialized")
	}
	settler := settlement.NewSettler(m.Repo.DB)
	if _, err := settler.Settle(ctx, ""); err != nil {
		return err
	}
	autoBetService := autobet.NewService(m.Repo.DB)
	_, err := autoBetService.Settle(ctx)
	return err
}

// Run loops independently of dashboard requests. The monitor uses the
// adaptive plan after each cycle and stops promptly on context cancellation.
func (m *Monitor) Run(ctx context.Context) error {
	if m == nil {
		return errors.New("live monitor is nil")
	}
	pollInterval := m.Config.normalize().PollInterval
	for {
		_, _ = m.PollOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
