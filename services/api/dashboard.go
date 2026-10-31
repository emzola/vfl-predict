package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type seasonView struct {
	SeasonID     string     `json:"season_id"`
	ProviderID   string     `json:"provider_season_id"`
	Year         string     `json:"provider_year,omitempty"`
	Abbreviation string     `json:"abbreviation,omitempty"`
	Current      bool       `json:"current"`
	StartsAt     *time.Time `json:"starts_at,omitempty"`
	EndsAt       *time.Time `json:"ends_at,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
}

type roundView struct {
	SeasonID            string     `json:"season_id"`
	Round               int        `json:"round"`
	EarliestKickoffAt   *time.Time `json:"earliest_kickoff_at,omitempty"`
	BettingCutoffAt     *time.Time `json:"betting_cutoff_at,omitempty"`
	State               string     `json:"state"`
	FixtureCount        int        `json:"fixture_count"`
	CompletedCount      int        `json:"completed_count"`
	LastFixtureObserved *time.Time `json:"last_fixture_observed_at,omitempty"`
	LastOddsObserved    *time.Time `json:"last_odds_observed_at,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

type fixtureView struct {
	MatchID     int64     `json:"match_id"`
	SeasonID    string    `json:"season_id"`
	Round       int       `json:"round"`
	ScheduledAt time.Time `json:"scheduled_at"`
	HomeTeamUID int64     `json:"home_team_uid"`
	HomeTeam    string    `json:"home_team"`
	AwayTeamUID int64     `json:"away_team_uid"`
	AwayTeam    string    `json:"away_team"`
	Status      string    `json:"status"`
	HomeScore   *int      `json:"home_score,omitempty"`
	AwayScore   *int      `json:"away_score,omitempty"`
}

type probabilityView struct {
	Home float64 `json:"home"`
	Draw float64 `json:"draw"`
	Away float64 `json:"away"`
}

type bookmakerView struct {
	HomeOdds        float64         `json:"home_odds"`
	DrawOdds        float64         `json:"draw_odds"`
	AwayOdds        float64         `json:"away_odds"`
	Implied         probabilityView `json:"implied"`
	ModelDifference probabilityView `json:"model_difference"`
	Overround       float64         `json:"overround"`
}

type goalMarketProbabilityView struct {
	Over25  float64 `json:"over_2_5"`
	Under25 float64 `json:"under_2_5"`
}

type bookmakerGoalMarketView struct {
	MarketID            int64                     `json:"market_id"`
	Specifiers          string                    `json:"specifiers"`
	OverOdds            float64                   `json:"over_odds"`
	UnderOdds           float64                   `json:"under_odds"`
	Implied             goalMarketProbabilityView `json:"implied"`
	ModelDifference     float64                   `json:"model_difference"`
	Overround           float64                   `json:"overround"`
	PointInTimeVerified bool                      `json:"point_in_time_verified"`
	ObservationKind     string                    `json:"observation_kind"`
}

type predictionView struct {
	PredictionRunID      string                   `json:"prediction_run_id"`
	PredictionRevisionID string                   `json:"prediction_revision_id"`
	MatchID              int64                    `json:"match_id"`
	SeasonID             string                   `json:"season_id"`
	Round                int                      `json:"round"`
	Fixture              fixtureView              `json:"fixture"`
	Probability          probabilityView          `json:"probability"`
	MostLikelyOutcome    string                   `json:"most_likely_outcome"`
	ModelVersion         string                   `json:"model_version"`
	Engine               string                   `json:"engine,omitempty"`
	EngineVersion        string                   `json:"engine_version,omitempty"`
	PredictionPolicy     string                   `json:"prediction_policy,omitempty"`
	Status               string                   `json:"status"`
	PredictionAsOf       time.Time                `json:"prediction_as_of"`
	CutoffAt             time.Time                `json:"cutoff_at"`
	EarliestKickoffAt    time.Time                `json:"earliest_kickoff_at"`
	OddsObservedAt       *time.Time               `json:"odds_observed_at,omitempty"`
	ProviderOddsTime     *time.Time               `json:"provider_odds_timestamp_at,omitempty"`
	PointInTimeVerified  bool                     `json:"point_in_time_verified"`
	Eligible             bool                     `json:"eligible"`
	IneligibleReason     string                   `json:"ineligible_reason,omitempty"`
	CreatedAt            time.Time                `json:"created_at"`
	Bookmaker            *bookmakerView           `json:"bookmaker,omitempty"`
	BookmakerGoalMarket  *bookmakerGoalMarketView `json:"bookmaker_goal_market,omitempty"`
	Secondary            map[string]any           `json:"secondary,omitempty"`
	SecondaryModel       string                   `json:"secondary_model,omitempty"`
	DynamicTeamStrength  map[string]any           `json:"dynamic_team_strength,omitempty"`
	FeatureValues        map[string]float64       `json:"feature_values,omitempty"`
	ActualOutcome        string                   `json:"actual_outcome,omitempty"`
	Correct              *bool                    `json:"correct,omitempty"`
	SettlementTrack      string                   `json:"settlement_track,omitempty"`
}

type predictionPayload struct {
	Model            string `json:"model"`
	ModelVersion     string `json:"model_version"`
	PredictionPolicy string `json:"prediction_policy"`
	Bookmaker        *struct {
		Home      float64         `json:"home"`
		Draw      float64         `json:"draw"`
		Away      float64         `json:"away"`
		Implied   probabilityView `json:"implied"`
		Overround float64         `json:"overround"`
	} `json:"bookmaker"`
	BookmakerGoalMarket *struct {
		MarketID            int64                     `json:"market_id"`
		Specifiers          string                    `json:"specifiers"`
		OverOdds            float64                   `json:"over_odds"`
		UnderOdds           float64                   `json:"under_odds"`
		Implied             goalMarketProbabilityView `json:"implied"`
		ModelDifference     float64                   `json:"model_difference"`
		Overround           float64                   `json:"overround"`
		PointInTimeVerified bool                      `json:"point_in_time_verified"`
		ObservationKind     string                    `json:"observation_kind"`
	} `json:"bookmaker_goal_market"`
	Secondary           map[string]any     `json:"secondary"`
	SecondaryModel      string             `json:"secondary_model"`
	DynamicTeamStrength map[string]any     `json:"dynamic_team_strength"`
	FeatureValues       map[string]float64 `json:"feature_values"`
}

type predictionPage struct {
	SeasonID          string           `json:"season_id,omitempty"`
	Round             int              `json:"round,omitempty"`
	Status            string           `json:"status,omitempty"`
	ExpectedFixtures  int              `json:"expected_fixture_count"`
	GeneratedFixtures int              `json:"generated_fixture_count"`
	ModelVersion      string           `json:"model_version,omitempty"`
	PredictionAsOf    *time.Time       `json:"prediction_as_of,omitempty"`
	CutoffAt          *time.Time       `json:"cutoff_at,omitempty"`
	Fixtures          []predictionView `json:"fixtures"`
	Complete          bool             `json:"complete"`
	FailureReason     string           `json:"failure_reason,omitempty"`
	Provenance        string           `json:"provenance"`
}

type metricView struct {
	Source            string   `json:"source"`
	RunID             string   `json:"run_id"`
	ModelDefinitionID int64    `json:"model_definition_id"`
	ModelKey          string   `json:"model_key,omitempty"`
	ModelVersion      string   `json:"model_version,omitempty"`
	Scope             string   `json:"scope"`
	SeasonID          string   `json:"season_id,omitempty"`
	Round             int      `json:"round,omitempty"`
	Market            string   `json:"market,omitempty"`
	Metric            string   `json:"metric"`
	Value             float64  `json:"value"`
	SampleCount       int      `json:"sample_count"`
	ConfidenceLow     *float64 `json:"confidence_low,omitempty"`
	ConfidenceHigh    *float64 `json:"confidence_high,omitempty"`
	PromotionStatus   string   `json:"promotion_status,omitempty"`
}

type forwardSummary struct {
	EligiblePredictions int      `json:"eligible_predictions"`
	SettledFixtures     int      `json:"settled_fixtures"`
	PendingFixtures     int      `json:"pending_fixtures"`
	LogLoss             *float64 `json:"log_loss,omitempty"`
	BrierScore          *float64 `json:"brier_score,omitempty"`
	Accuracy            *float64 `json:"accuracy,omitempty"`
	ROI                 *float64 `json:"roi,omitempty"`
	HasGenuineData      bool     `json:"has_genuine_data"`
	Provenance          string   `json:"provenance"`
}

type dashboardOverview struct {
	CheckedAt            time.Time        `json:"checked_at"`
	CurrentSeason        *seasonView      `json:"current_season,omitempty"`
	CurrentRound         *roundView       `json:"current_round,omitempty"`
	UpcomingFixtures     []fixtureView    `json:"upcoming_fixtures"`
	CompleteFrozenRounds int              `json:"complete_frozen_rounds"`
	LatestSettledRound   *roundView       `json:"latest_settled_round,omitempty"`
	FeedFreshness        map[string]any   `json:"feed_freshness"`
	WorkerStatus         []map[string]any `json:"worker_status"`
	ActiveModel          map[string]any   `json:"active_model,omitempty"`
	ForwardTesting       forwardSummary   `json:"forward_testing"`
}

type matchDetail struct {
	Fixture      fixtureView      `json:"fixture"`
	Odds         []map[string]any `json:"odds"`
	Observations []map[string]any `json:"observations"`
	Predictions  []predictionView `json:"predictions"`
	Provenance   string           `json:"provenance"`
}

type predictionGroup struct {
	SeasonID   string           `json:"season_id"`
	Round      int              `json:"round"`
	Status     string           `json:"status"`
	Model      string           `json:"model_version"`
	AsOf       *time.Time       `json:"prediction_as_of,omitempty"`
	Cutoff     *time.Time       `json:"cutoff_at,omitempty"`
	Fixtures   []predictionView `json:"fixtures"`
	Complete   bool             `json:"complete"`
	Provenance string           `json:"provenance"`
}

func (s *Server) dashboardRouter(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/dashboard/overview":
		s.overviewAPI(w, r)
	case "/api/v1/dashboard/future-rounds":
		s.futureRoundsAPI(w, r)
	case "/api/v1/dashboard/history":
		s.historyAPI(w, r)
	default:
		writeError(w, http.StatusNotFound, "dashboard resource not found")
	}
}

func (s *Server) overviewAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	result := dashboardOverview{CheckedAt: s.now(), UpcomingFixtures: make([]fixtureView, 0), FeedFreshness: map[string]any{}, WorkerStatus: make([]map[string]any, 0)}
	season, err := s.loadCurrentSeason(r)
	if err != nil {
		writeServerError(w, "could not load current season")
		return
	}
	result.CurrentSeason = season
	if season != nil {
		rounds, err := s.loadRounds(r, season.SeasonID)
		if err != nil {
			writeServerError(w, "could not load current round")
			return
		}
		result.CurrentRound = nextUpcomingRound(rounds, s.now())
		for index := range rounds {
			if rounds[index].State == "settled" {
				copyRound := rounds[index]
				result.LatestSettledRound = &copyRound
			}
		}
		if result.CurrentRound != nil {
			fixtures, fixtureErr := s.loadFixtures(r, season.SeasonID, result.CurrentRound.Round, true)
			if fixtureErr != nil {
				writeServerError(w, "could not load upcoming fixtures")
				return
			}
			result.UpcomingFixtures = fixtures
		}
	}
	if err := s.DB.QueryRowContext(r.Context(), "SELECT count(*) FROM round_states WHERE state = 'frozen' AND (SELECT count(*) FROM prediction_runs p WHERE p.season_id = round_states.season_id AND p.round_no = round_states.round_no AND p.status = 'frozen' AND p.generated_fixture_count = 8) > 0").Scan(&result.CompleteFrozenRounds); err != nil {
		writeServerError(w, "could not load prediction readiness")
		return
	}
	var latestObservation, latestFixture, latestUpdate sql.NullTime
	var observationCount, failedJobs int
	if err := s.DB.QueryRowContext(r.Context(), "SELECT max(observed_at), count(*) FROM live_feed_observations").Scan(&latestObservation, &observationCount); err != nil {
		writeServerError(w, "could not load feed freshness")
		return
	}
	if err := s.DB.QueryRowContext(r.Context(), "SELECT max(status_observed_at), max(updated_at) FROM fixtures").Scan(&latestFixture, &latestUpdate); err != nil {
		writeServerError(w, "could not load fixture freshness")
		return
	}
	if err := s.DB.QueryRowContext(r.Context(), "SELECT count(*) FILTER (WHERE status = 'failed') FROM job_execution_history").Scan(&failedJobs); err != nil {
		writeServerError(w, "could not load worker status")
		return
	}
	result.FeedFreshness = map[string]any{"latest_feed_observation": nullableTimeJSON(latestObservation), "observation_count": observationCount, "latest_fixture_observation": nullableTimeJSON(latestFixture), "latest_database_update": nullableTimeJSON(latestUpdate), "checked_at": s.now(), "stale_after_seconds": 900}
	result.WorkerStatus = []map[string]any{{"failed_jobs": failedJobs}}
	result.ActiveModel, _ = s.loadActiveModel(r)
	result.ForwardTesting, _ = s.loadForwardSummary(r)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) seasons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT season_id, provider_season_id, COALESCE(provider_year, ''), COALESCE(abbreviation, ''), current_flag, starts_at, ends_at, last_seen_at FROM seasons ORDER BY COALESCE(starts_at, last_seen_at) DESC LIMIT 100")
	if err != nil {
		writeServerError(w, "could not load seasons")
		return
	}
	defer rows.Close()
	items := make([]seasonView, 0)
	for rows.Next() {
		item, err := scanSeason(rows)
		if err != nil {
			writeServerError(w, "could not load seasons")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"seasons": items})
}

func (s *Server) seasonRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/seasons/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[1] != "rounds" || !isSeasonID(parts[0]) {
		writeError(w, http.StatusNotFound, "season resource not found")
		return
	}
	rounds, err := s.loadRounds(r, parts[0])
	if err != nil {
		writeServerError(w, "could not load rounds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"season_id": parts[0], "rounds": rounds})
}

func (s *Server) predictionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	seasonID := strings.TrimSpace(r.URL.Query().Get("season_id"))
	if seasonID != "" && !isSeasonID(seasonID) {
		writeError(w, http.StatusBadRequest, "invalid season id")
		return
	}
	round := 0
	if value := strings.TrimSpace(r.URL.Query().Get("round")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 30 {
			writeError(w, http.StatusBadRequest, "invalid round")
			return
		}
		round = parsed
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if !validPredictionStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid prediction status")
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("prediction_run_id"))
	if len(runID) > 64 {
		writeError(w, http.StatusBadRequest, "invalid prediction id")
		return
	}
	page, err := s.loadPredictionPage(r, seasonID, round, status, runID, 512, false)
	if err != nil {
		writeServerError(w, "could not load predictions")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) predictionRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	runID := strings.TrimPrefix(r.URL.Path, "/api/v1/predictions/")
	if len(runID) < 8 || strings.Contains(runID, "/") {
		writeError(w, http.StatusBadRequest, "invalid prediction id")
		return
	}
	page, err := s.loadPredictionPage(r, "", 0, "", runID, 512, false)
	if err != nil {
		writeServerError(w, "could not load prediction")
		return
	}
	if len(page.Fixtures) == 0 {
		writeError(w, http.StatusNotFound, "prediction not found")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) futureRoundsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	seasonID := strings.TrimSpace(r.URL.Query().Get("season_id"))
	if seasonID != "" && !isSeasonID(seasonID) {
		writeError(w, http.StatusBadRequest, "invalid season id")
		return
	}
	page, err := s.loadPredictionPage(r, seasonID, 0, "provisional", "", 256, true)
	if err != nil {
		writeServerError(w, "could not load future rounds")
		return
	}
	groups := make(map[string]*predictionGroup)
	for _, item := range page.Fixtures {
		key := item.SeasonID + ":" + strconv.Itoa(item.Round)
		group := groups[key]
		if group == nil {
			group = &predictionGroup{
				SeasonID:   item.SeasonID,
				Round:      item.Round,
				Status:     item.Status,
				Model:      item.ModelVersion,
				Fixtures:   make([]predictionView, 0, 8),
				Provenance: page.Provenance,
			}
			groups[key] = group
		}
		group.Fixtures = append(group.Fixtures, item)
		if group.AsOf == nil || item.PredictionAsOf.After(*group.AsOf) {
			asOf := item.PredictionAsOf
			cutoff := item.CutoffAt
			group.AsOf, group.Cutoff = &asOf, &cutoff
		}
	}
	result := make([]predictionGroup, 0, len(groups))
	for _, group := range groups {
		group.Complete = len(group.Fixtures) == 8
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SeasonID != result[j].SeasonID {
			return result[i].SeasonID < result[j].SeasonID
		}
		return result[i].Round < result[j].Round
	})
	writeJSON(w, http.StatusOK, map[string]any{"rounds": result, "provenance": page.Provenance})
}

func (s *Server) historyAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page := 1
	if value := strings.TrimSpace(r.URL.Query().Get("page")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 1_000_000 {
			writeError(w, http.StatusBadRequest, "invalid history pagination")
			return
		}
		page = parsed
	}
	pageSize := 10
	if value := strings.TrimSpace(r.URL.Query().Get("page_size")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "invalid history pagination")
			return
		}
		pageSize = parsed
	}
	items, hasNext, err := s.loadPredictionHistory(r, pageSize, (page-1)*pageSize)
	if err != nil {
		if errors.Is(err, errInvalidAuthInput) {
			writeError(w, http.StatusBadRequest, "invalid history filter")
			return
		}
		writeServerError(w, "could not load prediction history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"predictions": items, "page": page, "page_size": pageSize, "has_next": hasNext, "provenance": "stored prediction revisions and settlements"})
}

func (s *Server) matchRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	matchID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/matches/"), 10, 64)
	if err != nil || matchID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	result, err := s.loadMatchDetail(r, matchID)
	if err != nil {
		writeServerError(w, "could not load match")
		return
	}
	if result == nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) matchOddsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	matchID, err := strconv.ParseInt(r.PathValue("matchId"), 10, 64)
	if err != nil || matchID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	result, err := s.loadMatchDetail(r, matchID)
	if err != nil {
		writeServerError(w, "could not load odds")
		return
	}
	if result == nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"match_id": matchID, "odds": result.Odds, "provenance": result.Provenance})
}

func (s *Server) roundFixturesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	seasonID := r.PathValue("seasonId")
	round, err := strconv.Atoi(r.PathValue("round"))
	if !isSeasonID(seasonID) || err != nil || round < 1 || round > 30 {
		writeError(w, http.StatusBadRequest, "invalid season or round")
		return
	}
	fixtures, err := s.loadFixtures(r, seasonID, round, false)
	if err != nil {
		writeServerError(w, "could not load fixtures")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"season_id": seasonID, "round": round, "fixtures": fixtures, "complete": len(fixtures) == 8})
}

func (s *Server) systemHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status := "ok"
	if s.DB == nil {
		status = "unavailable"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "phase": "phase4", "provider_connectivity": "unvalidated", "checked_at": s.now()})
}

func (s *Server) performanceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	metrics, err := s.loadMetrics(r, "retrospective")
	if err != nil {
		writeServerError(w, "could not load performance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics, "datasets": []string{"retrospective_historical_evaluation", "genuine_forward_observed", "offline_verification"}, "provenance": "research metrics are kept separate from forward observations"})
}

func (s *Server) paperTestingAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	summary, err := s.loadForwardSummary(r)
	if err != nil {
		writeServerError(w, "could not load paper testing")
		return
	}
	rows, err := s.loadMetrics(r, "forward_observed")
	if err != nil {
		writeServerError(w, "could not load paper metrics")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": summary, "metrics": rows, "provenance": "only forward_observed settlement track and eligible pre-cutoff odds"})
}

func (s *Server) operationsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	result := map[string]any{"checked_at": s.now(), "leases": []map[string]any{}, "jobs": []map[string]any{}, "coverage": []map[string]any{}, "quarantine": map[string]any{}}
	var latestObservation sql.NullTime
	var observationCount int
	if err := s.DB.QueryRowContext(r.Context(), "SELECT max(observed_at), count(*) FROM live_feed_observations").Scan(&latestObservation, &observationCount); err != nil {
		writeServerError(w, "could not load operations")
		return
	}
	result["feed"] = map[string]any{"latest_observation_at": nullableTimeJSON(latestObservation), "observation_count": observationCount}
	leaseRows, err := s.DB.QueryContext(r.Context(), "SELECT lease_key, owner_id, lease_until, heartbeat_at FROM scheduler_leases ORDER BY lease_key LIMIT 32")
	if err == nil {
		defer leaseRows.Close()
		leases := make([]map[string]any, 0)
		for leaseRows.Next() {
			var key, owner string
			var until, heartbeat time.Time
			if leaseRows.Scan(&key, &owner, &until, &heartbeat) == nil {
				leases = append(leases, map[string]any{"lease_key": key, "owner_id": owner, "lease_until": until.UTC(), "heartbeat_at": heartbeat.UTC(), "active": until.After(s.now())})
			}
		}
		result["leases"] = leases
	}
	jobRows, err := s.DB.QueryContext(r.Context(), "SELECT job_key, worker_name, status, started_at, finished_at, error_code, error_message FROM job_execution_history ORDER BY started_at DESC LIMIT 50")
	if err == nil {
		defer jobRows.Close()
		jobs := make([]map[string]any, 0)
		for jobRows.Next() {
			var key, worker, status string
			var started time.Time
			var finished sql.NullTime
			var errorCode, errorMessage sql.NullString
			if jobRows.Scan(&key, &worker, &status, &started, &finished, &errorCode, &errorMessage) == nil {
				item := map[string]any{"job_key": key, "worker_name": worker, "status": status, "started_at": started.UTC()}
				if finished.Valid {
					item["finished_at"] = finished.Time.UTC()
				}
				if errorCode.Valid {
					item["error_code"] = errorCode.String
				}
				if errorMessage.Valid {
					item["error_message"] = errorMessage.String
				}
				jobs = append(jobs, item)
			}
		}
		result["jobs"] = jobs
	}
	coverageRows, err := s.DB.QueryContext(r.Context(), "SELECT season_id, round_no, fixture_count, scored_fixture_count, completed_fixture_count, void_fixture_count FROM v_fixture_coverage ORDER BY season_id DESC, round_no LIMIT 300")
	if err == nil {
		defer coverageRows.Close()
		coverage := make([]map[string]any, 0)
		for coverageRows.Next() {
			var season string
			var round, fixtures, scored, completed, voided int
			if coverageRows.Scan(&season, &round, &fixtures, &scored, &completed, &voided) == nil {
				coverage = append(coverage, map[string]any{"season_id": season, "round": round, "fixture_count": fixtures, "scored_fixture_count": scored, "completed_fixture_count": completed, "void_fixture_count": voided})
			}
		}
		result["coverage"] = coverage
	}
	var openQuarantine, totalQuarantine int
	_ = s.DB.QueryRowContext(r.Context(), "SELECT count(*) FILTER (WHERE resolved_at IS NULL), count(*) FROM validation_errors").Scan(&openQuarantine, &totalQuarantine)
	result["quarantine"] = map[string]any{"open": openQuarantine, "total": totalQuarantine}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) settingsAPI(w http.ResponseWriter, r *http.Request) {
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		var timezone, theme, dateFormat string
		var refresh int
		if err := s.DB.QueryRowContext(r.Context(), "SELECT timezone, theme, refresh_interval_seconds, date_format FROM user_preferences WHERE account_id = $1", session.AccountID).Scan(&timezone, &theme, &refresh, &dateFormat); err != nil {
			writeServerError(w, "could not load settings")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"email": session.Email, "preferences": map[string]any{"timezone": timezone, "theme": theme, "refresh_interval_seconds": refresh, "date_format": dateFormat}})
	case http.MethodPatch:
		if !s.csrfRequestIsValid(r, session) {
			writeError(w, http.StatusForbidden, "csrf validation failed")
			return
		}
		var input struct {
			Timezone               string `json:"timezone"`
			Theme                  string `json:"theme"`
			RefreshIntervalSeconds int    `json:"refresh_interval_seconds"`
			DateFormat             string `json:"date_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid settings")
			return
		}
		if _, err := time.LoadLocation(strings.TrimSpace(input.Timezone)); err != nil {
			writeError(w, http.StatusBadRequest, "invalid timezone")
			return
		}
		if input.Theme != "dark" && input.Theme != "light" && input.Theme != "system" {
			writeError(w, http.StatusBadRequest, "invalid theme")
			return
		}
		if input.RefreshIntervalSeconds < 30 || input.RefreshIntervalSeconds > 900 {
			writeError(w, http.StatusBadRequest, "invalid refresh interval")
			return
		}
		if input.DateFormat != "iso" && input.DateFormat != "locale" {
			writeError(w, http.StatusBadRequest, "invalid date format")
			return
		}
		if _, err := s.DB.ExecContext(r.Context(), "UPDATE user_preferences SET timezone = $2, theme = $3, refresh_interval_seconds = $4, date_format = $5, updated_at = $6 WHERE account_id = $1", session.AccountID, truncate(input.Timezone, 80), input.Theme, input.RefreshIntervalSeconds, input.DateFormat, s.now()); err != nil {
			writeServerError(w, "could not update settings")
			return
		}
		_ = s.recordAuthAudit(r.Context(), "settings_updated", session.AccountID, hashSecret(session.Email), true, nil, r)
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) loadCurrentSeason(r *http.Request) (*seasonView, error) {
	query := "SELECT season_id, provider_season_id, COALESCE(provider_year, ''), COALESCE(abbreviation, ''), current_flag, starts_at, ends_at, last_seen_at FROM seasons WHERE season_id = COALESCE((SELECT season_id FROM seasons WHERE current_flag = true ORDER BY last_seen_at DESC LIMIT 1), (SELECT season_id FROM live_feed_observations WHERE endpoint_family = 'live_season_fixtures' AND parse_status = 'accepted' ORDER BY observed_at DESC LIMIT 1)) LIMIT 1"
	item, err := scanSeasonRow(s.DB.QueryRowContext(r.Context(), query))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSeason(rows *sql.Rows) (seasonView, error) {
	return scanSeasonRow(rows)
}

func scanSeasonRow(row scanner) (seasonView, error) {
	var item seasonView
	var starts, ends, last sql.NullTime
	if err := row.Scan(&item.SeasonID, &item.ProviderID, &item.Year, &item.Abbreviation, &item.Current, &starts, &ends, &last); err != nil {
		return seasonView{}, err
	}
	item.StartsAt, item.EndsAt, item.LastSeenAt = nullableTime(starts), nullableTime(ends), nullableTime(last)
	return item, nil
}

func (s *Server) loadRounds(r *http.Request, seasonID string) ([]roundView, error) {
	query := "SELECT rs.season_id, rs.round_no, rs.earliest_kickoff_at, rs.betting_cutoff_at, rs.state, rs.last_fixture_observed_at, rs.last_odds_observed_at, rs.updated_at, count(f.match_id), count(f.match_id) FILTER (WHERE f.result_home IS NOT NULL AND f.result_away IS NOT NULL) FROM round_states rs LEFT JOIN fixtures f ON f.season_id = rs.season_id AND f.round_no = rs.round_no WHERE rs.season_id = $1 GROUP BY rs.season_id, rs.round_no, rs.earliest_kickoff_at, rs.betting_cutoff_at, rs.state, rs.last_fixture_observed_at, rs.last_odds_observed_at, rs.updated_at ORDER BY rs.round_no"
	rows, err := s.DB.QueryContext(r.Context(), query, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]roundView, 0)
	for rows.Next() {
		var item roundView
		var kickoff, cutoff, fixtureSeen, oddsSeen, updated sql.NullTime
		if err := rows.Scan(&item.SeasonID, &item.Round, &kickoff, &cutoff, &item.State, &fixtureSeen, &oddsSeen, &updated, &item.FixtureCount, &item.CompletedCount); err != nil {
			return nil, err
		}
		item.EarliestKickoffAt, item.BettingCutoffAt, item.LastFixtureObserved, item.LastOddsObserved, item.UpdatedAt = nullableTime(kickoff), nullableTime(cutoff), nullableTime(fixtureSeen), nullableTime(oddsSeen), nullableTime(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}

// nextUpcomingRound mirrors the prediction worker's selection rule: an
// in-progress round is not a valid target for a new prediction run. The
// caller supplies rounds ordered by round number, so the first eligible round
// is the next round after the currently playing round.
func nextUpcomingRound(rounds []roundView, now time.Time) *roundView {
	for index := range rounds {
		item := rounds[index]
		if item.State == "settled" || item.State == "missed_cutoff" || item.EarliestKickoffAt == nil {
			continue
		}
		if !item.EarliestKickoffAt.Before(now) {
			return &item
		}
	}
	return nil
}

func (s *Server) loadFixtures(r *http.Request, seasonID string, round int, upcomingOnly bool) ([]fixtureView, error) {
	query := "SELECT f.match_id, f.season_id, f.round_no, f.scheduled_at, f.home_team_uid, ht.name, f.away_team_uid, at.name, f.status, f.result_home, f.result_away FROM fixtures f JOIN teams ht ON ht.team_uid = f.home_team_uid JOIN teams at ON at.team_uid = f.away_team_uid WHERE f.season_id = $1 AND f.round_no = $2"
	if upcomingOnly {
		query += " AND f.scheduled_at >= now() AND f.status NOT IN ('completed', 'canceled')"
	}
	query += " ORDER BY f.scheduled_at, f.match_id LIMIT 16"
	rows, err := s.DB.QueryContext(r.Context(), query, seasonID, round)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]fixtureView, 0)
	for rows.Next() {
		item, err := scanFixture(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanFixture(row scanner) (fixtureView, error) {
	var item fixtureView
	var homeScore, awayScore sql.NullInt64
	if err := row.Scan(&item.MatchID, &item.SeasonID, &item.Round, &item.ScheduledAt, &item.HomeTeamUID, &item.HomeTeam, &item.AwayTeamUID, &item.AwayTeam, &item.Status, &homeScore, &awayScore); err != nil {
		return fixtureView{}, err
	}
	if homeScore.Valid {
		value := int(homeScore.Int64)
		item.HomeScore = &value
	}
	if awayScore.Valid {
		value := int(awayScore.Int64)
		item.AwayScore = &value
	}
	item.ScheduledAt = item.ScheduledAt.UTC()
	return item, nil
}

func (s *Server) loadPredictionPage(r *http.Request, seasonID string, round int, status, runID string, limit int, futureOnly bool) (predictionPage, error) {
	futureFilter := ""
	if futureOnly {
		futureFilter = " AND p.earliest_kickoff_at >= now() AND NOT EXISTS (SELECT 1 FROM prediction_runs official WHERE official.season_id = p.season_id AND official.round_no = p.round_no AND official.status IN ('frozen', 'settled') AND official.generated_fixture_count = 8)"
	}
	query := "SELECT DISTINCT ON (p.season_id, p.round_no, pr.match_id) p.prediction_run_id::text, pr.prediction_revision_id::text, p.season_id, p.round_no, p.model_version, p.status, p.prediction_as_of, p.input_cutoff_at, p.earliest_kickoff_at, p.expected_fixture_count, p.generated_fixture_count, pr.match_id, f.scheduled_at, f.home_team_uid, ht.name, f.away_team_uid, at.name, f.status, f.result_home, f.result_away, pr.home_probability, pr.draw_probability, pr.away_probability, pr.probability_payload, pr.odds_observed_at, pr.provider_odds_timestamp_at, pr.point_in_time_verified, pr.eligible, COALESCE(pr.ineligible_reason, ''), pr.created_at, settlement.actual_outcome, settlement.correct, settlement.settlement_track FROM prediction_runs p JOIN prediction_revisions pr ON pr.prediction_run_id = p.prediction_run_id JOIN fixtures f ON f.match_id = pr.match_id JOIN teams ht ON ht.team_uid = f.home_team_uid JOIN teams at ON at.team_uid = f.away_team_uid LEFT JOIN LATERAL (SELECT actual_outcome, correct, settlement_track FROM prediction_settlements ps WHERE ps.prediction_revision_id = pr.prediction_revision_id ORDER BY correction_version DESC LIMIT 1) settlement ON TRUE WHERE ($1 = '' OR p.season_id = $1) AND ($2 = 0 OR p.round_no = $2) AND ($3 = '' OR p.status = $3) AND ($4 = '' OR p.prediction_run_id::text = $4)" + futureFilter + " ORDER BY p.season_id, p.round_no, pr.match_id, CASE p.status WHEN 'settled' THEN 0 WHEN 'frozen' THEN 1 WHEN 'provisional' THEN 2 ELSE 3 END, p.prediction_as_of DESC, pr.revision_no DESC LIMIT $5"
	rows, err := s.DB.QueryContext(r.Context(), query, seasonID, round, status, runID, limit)
	if err != nil {
		return predictionPage{}, err
	}
	defer rows.Close()
	page := predictionPage{SeasonID: seasonID, Round: round, Fixtures: make([]predictionView, 0), Provenance: "stored prediction revisions; odds timestamps are application observations when present"}
	for rows.Next() {
		item, expected, generated, err := scanPredictionView(rows)
		if err != nil {
			return predictionPage{}, err
		}
		if page.SeasonID == "" {
			page.SeasonID = item.SeasonID
		}
		if page.Round == 0 {
			page.Round = item.Round
		}
		page.ExpectedFixtures, page.GeneratedFixtures, page.Status, page.ModelVersion = expected, generated, item.Status, item.ModelVersion
		copyAsOf, copyCutoff := item.PredictionAsOf, item.CutoffAt
		page.PredictionAsOf, page.CutoffAt = &copyAsOf, &copyCutoff
		page.Fixtures = append(page.Fixtures, item)
	}
	page.Complete = page.ExpectedFixtures == 8 && page.GeneratedFixtures == 8 && len(page.Fixtures) == 8
	// An unavailable or missed run has no revision rows by design. Preserve its
	// status and failure reason so the dashboard does not reduce an auditable
	// worker outcome to the ambiguous label "unavailable".
	if len(page.Fixtures) == 0 && seasonID != "" && round > 0 {
		var status, modelVersion string
		var predictionAsOf, cutoff time.Time
		var expected, generated int
		var failureReason sql.NullString
		err := s.DB.QueryRowContext(r.Context(), `
SELECT status, model_version, prediction_as_of, input_cutoff_at,
       expected_fixture_count, generated_fixture_count, failure_reason
FROM prediction_runs
WHERE season_id = $1 AND round_no = $2
ORDER BY prediction_as_of DESC, created_at DESC
LIMIT 1`, seasonID, round).Scan(&status, &modelVersion, &predictionAsOf, &cutoff, &expected, &generated, &failureReason)
		if err == nil {
			page.Status = status
			page.ModelVersion = modelVersion
			page.ExpectedFixtures = expected
			page.GeneratedFixtures = generated
			predictionAsOf = predictionAsOf.UTC()
			cutoff = cutoff.UTC()
			page.PredictionAsOf = &predictionAsOf
			page.CutoffAt = &cutoff
			if failureReason.Valid {
				page.FailureReason = failureReason.String
			}
		}
	}
	return page, rows.Err()
}

func scanPredictionView(row scanner) (predictionView, int, int, error) {
	var item predictionView
	var expected, generated int
	var fixture fixtureView
	var homeScore, awayScore sql.NullInt64
	var payload []byte
	var oddsAt, providerAt sql.NullTime
	var actualOutcome, settlementTrack sql.NullString
	var correct sql.NullBool
	if err := row.Scan(&item.PredictionRunID, &item.PredictionRevisionID, &item.SeasonID, &item.Round, &item.ModelVersion, &item.Status, &item.PredictionAsOf, &item.CutoffAt, &item.EarliestKickoffAt, &expected, &generated, &fixture.MatchID, &fixture.ScheduledAt, &fixture.HomeTeamUID, &fixture.HomeTeam, &fixture.AwayTeamUID, &fixture.AwayTeam, &fixture.Status, &homeScore, &awayScore, &item.Probability.Home, &item.Probability.Draw, &item.Probability.Away, &payload, &oddsAt, &providerAt, &item.PointInTimeVerified, &item.Eligible, &item.IneligibleReason, &item.CreatedAt, &actualOutcome, &correct, &settlementTrack); err != nil {
		return predictionView{}, 0, 0, err
	}
	fixture.SeasonID, fixture.Round = item.SeasonID, item.Round
	fixture.ScheduledAt = fixture.ScheduledAt.UTC()
	if homeScore.Valid {
		value := int(homeScore.Int64)
		fixture.HomeScore = &value
	}
	if awayScore.Valid {
		value := int(awayScore.Int64)
		fixture.AwayScore = &value
	}
	item.Fixture = fixture
	item.PredictionAsOf, item.CutoffAt, item.EarliestKickoffAt, item.CreatedAt = item.PredictionAsOf.UTC(), item.CutoffAt.UTC(), item.EarliestKickoffAt.UTC(), item.CreatedAt.UTC()
	if oddsAt.Valid {
		value := oddsAt.Time.UTC()
		item.OddsObservedAt = &value
	}
	if providerAt.Valid {
		value := providerAt.Time.UTC()
		item.ProviderOddsTime = &value
	}
	item.MostLikelyOutcome = mostLikelyOutcome(item.Probability)
	applyPredictionPayload(&item, payload)
	if actualOutcome.Valid {
		item.ActualOutcome = actualOutcome.String
	}
	if correct.Valid {
		value := correct.Bool
		item.Correct = &value
	}
	if settlementTrack.Valid {
		item.SettlementTrack = settlementTrack.String
	}
	return item, expected, generated, nil
}

func applyPredictionPayload(item *predictionView, payload []byte) {
	if item == nil || len(payload) == 0 {
		return
	}
	var decoded predictionPayload
	if json.Unmarshal(payload, &decoded) != nil {
		return
	}
	item.Engine = decoded.Model
	item.EngineVersion = decoded.ModelVersion
	item.PredictionPolicy = decoded.PredictionPolicy
	if decoded.Bookmaker != nil {
		item.Bookmaker = &bookmakerView{
			HomeOdds:  decoded.Bookmaker.Home,
			DrawOdds:  decoded.Bookmaker.Draw,
			AwayOdds:  decoded.Bookmaker.Away,
			Implied:   decoded.Bookmaker.Implied,
			Overround: decoded.Bookmaker.Overround,
			ModelDifference: probabilityView{
				Home: item.Probability.Home - decoded.Bookmaker.Implied.Home,
				Draw: item.Probability.Draw - decoded.Bookmaker.Implied.Draw,
				Away: item.Probability.Away - decoded.Bookmaker.Implied.Away,
			},
		}
	}
	item.Secondary = decoded.Secondary
	if decoded.BookmakerGoalMarket != nil {
		item.BookmakerGoalMarket = &bookmakerGoalMarketView{
			MarketID:            decoded.BookmakerGoalMarket.MarketID,
			Specifiers:          decoded.BookmakerGoalMarket.Specifiers,
			OverOdds:            decoded.BookmakerGoalMarket.OverOdds,
			UnderOdds:           decoded.BookmakerGoalMarket.UnderOdds,
			Implied:             decoded.BookmakerGoalMarket.Implied,
			ModelDifference:     decoded.BookmakerGoalMarket.ModelDifference,
			Overround:           decoded.BookmakerGoalMarket.Overround,
			PointInTimeVerified: decoded.BookmakerGoalMarket.PointInTimeVerified,
			ObservationKind:     decoded.BookmakerGoalMarket.ObservationKind,
		}
		if over25, ok := secondaryProbability(item.Secondary, "over_2_5"); ok && finiteDashboardValue(item.BookmakerGoalMarket.Implied.Over25) {
			item.BookmakerGoalMarket.ModelDifference = over25 - item.BookmakerGoalMarket.Implied.Over25
		}
	}
	item.SecondaryModel = decoded.SecondaryModel
	item.DynamicTeamStrength = decoded.DynamicTeamStrength
	if item.DynamicTeamStrength != nil {
		if item.Secondary == nil {
			item.Secondary = make(map[string]any)
		}
		item.Secondary["dynamic_team_strength"] = item.DynamicTeamStrength
	}
	item.FeatureValues = decoded.FeatureValues
}

func secondaryProbability(value map[string]any, key string) (float64, bool) {
	if value == nil {
		return 0, false
	}
	number, ok := value[key].(float64)
	return number, ok && finiteDashboardValue(number)
}

func finiteDashboardValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *Server) loadPredictionHistory(r *http.Request, limit, offset int) ([]predictionView, bool, error) {
	seasonID := strings.TrimSpace(r.URL.Query().Get("season_id"))
	if seasonID != "" && !isSeasonID(seasonID) {
		return nil, false, errInvalidAuthInput
	}
	round := 0
	if value := strings.TrimSpace(r.URL.Query().Get("round")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 30 {
			return nil, false, errInvalidAuthInput
		}
		round = parsed
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if !validPredictionStatus(status) {
		return nil, false, errInvalidAuthInput
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if len(model) > 128 {
		return nil, false, errInvalidAuthInput
	}
	query := "SELECT p.prediction_run_id::text, pr.prediction_revision_id::text, p.season_id, p.round_no, p.model_version, p.status, p.prediction_as_of, p.input_cutoff_at, p.earliest_kickoff_at, p.expected_fixture_count, p.generated_fixture_count, pr.match_id, f.scheduled_at, f.home_team_uid, ht.name, f.away_team_uid, at.name, f.status, f.result_home, f.result_away, pr.home_probability, pr.draw_probability, pr.away_probability, pr.probability_payload, pr.odds_observed_at, pr.provider_odds_timestamp_at, pr.point_in_time_verified, pr.eligible, COALESCE(pr.ineligible_reason, ''), pr.created_at, settlement.actual_outcome, settlement.correct, settlement.settlement_track FROM prediction_runs p JOIN prediction_revisions pr ON pr.prediction_run_id = p.prediction_run_id JOIN fixtures f ON f.match_id = pr.match_id JOIN teams ht ON ht.team_uid = f.home_team_uid JOIN teams at ON at.team_uid = f.away_team_uid LEFT JOIN LATERAL (SELECT actual_outcome, correct, settlement_track FROM prediction_settlements ps WHERE ps.prediction_revision_id = pr.prediction_revision_id ORDER BY correction_version DESC LIMIT 1) settlement ON TRUE WHERE ($1 = '' OR p.season_id = $1) AND ($2 = 0 OR p.round_no = $2) AND ($3 = '' OR p.status = $3) AND ($4 = '' OR p.model_version = $4) ORDER BY p.prediction_as_of DESC, pr.created_at DESC, pr.revision_no DESC LIMIT $5 OFFSET $6"
	rows, err := s.DB.QueryContext(r.Context(), query, seasonID, round, status, model, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]predictionView, 0, limit+1)
	for rows.Next() {
		item, _, _, err := scanPredictionView(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

func (s *Server) loadMatchDetail(r *http.Request, matchID int64) (*matchDetail, error) {
	row := s.DB.QueryRowContext(r.Context(), "SELECT f.match_id, f.season_id, f.round_no, f.scheduled_at, f.home_team_uid, ht.name, f.away_team_uid, at.name, f.status, f.result_home, f.result_away FROM fixtures f JOIN teams ht ON ht.team_uid = f.home_team_uid JOIN teams at ON at.team_uid = f.away_team_uid WHERE f.match_id = $1", matchID)
	fixture, err := scanFixture(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := &matchDetail{Fixture: fixture, Odds: make([]map[string]any, 0), Observations: make([]map[string]any, 0), Predictions: make([]predictionView, 0), Provenance: "odds are stored snapshots; availability depends on observation_kind and point_in_time_verified"}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT o.odds_snapshot_id::text, o.provider_market_id, o.specifiers, o.outcome_id, o.outcome_name, o.raw_odds, o.decimal_odds, o.market_status, o.outcome_active, o.observation_kind, o.point_in_time_verified, o.source_timestamp_at, o.first_observed_at, o.last_observed_at FROM odds_snapshots o WHERE o.match_id = $1 ORDER BY o.last_observed_at DESC LIMIT 256", matchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, specifiers, outcomeID, outcomeName, rawOdds, kind string
		var marketID, marketStatus, active int
		var odds sql.NullFloat64
		var verified bool
		var sourceAt, firstAt, lastAt time.Time
		if err := rows.Scan(&id, &marketID, &specifiers, &outcomeID, &outcomeName, &rawOdds, &odds, &marketStatus, &active, &kind, &verified, &sourceAt, &firstAt, &lastAt); err != nil {
			return nil, err
		}
		item := map[string]any{"odds_snapshot_id": id, "market_id": marketID, "specifiers": specifiers, "outcome_id": outcomeID, "outcome_name": outcomeName, "raw_odds": rawOdds, "market_status": marketStatus, "outcome_active": active, "observation_kind": kind, "point_in_time_verified": verified, "source_timestamp_at": sourceAt.UTC(), "first_observed_at": firstAt.UTC(), "last_observed_at": lastAt.UTC()}
		if odds.Valid {
			item["decimal_odds"] = odds.Float64
		}
		result.Odds = append(result.Odds, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	observationRows, err := s.DB.QueryContext(r.Context(), "SELECT oo.odds_observation_id::text, oo.odds_snapshot_id::text, oo.observed_at, oo.fetched_at, oo.source_timestamp_at, oo.observation_kind, oo.point_in_time_verified, oo.provenance FROM odds_snapshot_observations oo JOIN odds_snapshots o ON o.odds_snapshot_id = oo.odds_snapshot_id WHERE o.match_id = $1 ORDER BY oo.observed_at DESC LIMIT 256", matchID)
	if err != nil {
		return nil, err
	}
	for observationRows.Next() {
		var observationID, snapshotID, kind string
		var observedAt, fetchedAt, sourceAt time.Time
		var verified bool
		var provenance []byte
		if err := observationRows.Scan(&observationID, &snapshotID, &observedAt, &fetchedAt, &sourceAt, &kind, &verified, &provenance); err != nil {
			_ = observationRows.Close()
			return nil, err
		}
		item := map[string]any{"odds_observation_id": observationID, "odds_snapshot_id": snapshotID, "observed_at": observedAt.UTC(), "fetched_at": fetchedAt.UTC(), "source_timestamp_at": sourceAt.UTC(), "observation_kind": kind, "point_in_time_verified": verified}
		if len(provenance) > 0 {
			var decoded any
			if json.Unmarshal(provenance, &decoded) == nil {
				item["provenance"] = decoded
			}
		}
		result.Observations = append(result.Observations, item)
	}
	if err := observationRows.Err(); err != nil {
		_ = observationRows.Close()
		return nil, err
	}
	if err := observationRows.Close(); err != nil {
		return nil, err
	}
	result.Predictions, err = s.loadMatchPredictions(r, matchID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) loadMatchPredictions(r *http.Request, matchID int64) ([]predictionView, error) {
	query := "SELECT p.prediction_run_id::text, pr.prediction_revision_id::text, p.season_id, p.round_no, p.model_version, p.status, p.prediction_as_of, p.input_cutoff_at, p.earliest_kickoff_at, p.expected_fixture_count, p.generated_fixture_count, pr.match_id, f.scheduled_at, f.home_team_uid, ht.name, f.away_team_uid, at.name, f.status, f.result_home, f.result_away, pr.home_probability, pr.draw_probability, pr.away_probability, pr.probability_payload, pr.odds_observed_at, pr.provider_odds_timestamp_at, pr.point_in_time_verified, pr.eligible, COALESCE(pr.ineligible_reason, ''), pr.created_at, settlement.actual_outcome, settlement.correct, settlement.settlement_track FROM prediction_runs p JOIN prediction_revisions pr ON pr.prediction_run_id = p.prediction_run_id JOIN fixtures f ON f.match_id = pr.match_id JOIN teams ht ON ht.team_uid = f.home_team_uid JOIN teams at ON at.team_uid = f.away_team_uid LEFT JOIN LATERAL (SELECT actual_outcome, correct, settlement_track FROM prediction_settlements ps WHERE ps.prediction_revision_id = pr.prediction_revision_id ORDER BY correction_version DESC LIMIT 1) settlement ON TRUE WHERE pr.match_id = $1 ORDER BY p.prediction_as_of DESC, pr.revision_no DESC LIMIT 128"
	rows, err := s.DB.QueryContext(r.Context(), query, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]predictionView, 0)
	for rows.Next() {
		item, _, _, err := scanPredictionView(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadMetrics(r *http.Request, source string) ([]metricView, error) {
	if source == "forward_observed" {
		rows, err := s.DB.QueryContext(r.Context(), "SELECT forward_result_id::text, prediction_run_id::text, season_id, round_no, market_key, metric_key, metric_value, sample_count, eligible_odds_count FROM forward_paper_results ORDER BY created_at DESC LIMIT 500")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]metricView, 0)
		for rows.Next() {
			var item metricView
			var forwardID string
			var eligible int
			if err := rows.Scan(&forwardID, &item.RunID, &item.SeasonID, &item.Round, &item.Market, &item.Metric, &item.Value, &item.SampleCount, &eligible); err != nil {
				return nil, err
			}
			item.Source = source
			result = append(result, item)
		}
		return result, rows.Err()
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT e.backtest_run_id::text, e.model_definition_id, COALESCE(m.model_key, ''), COALESCE(m.version, ''), e.scope_type, COALESCE(e.scope_season_id, ''), COALESCE(e.scope_round_no, 0), COALESCE(e.market_key, ''), e.metric_key, e.metric_value, e.sample_count, e.confidence_low, e.confidence_high, COALESCE(a.promotion_status, '') FROM evaluation_metrics e JOIN model_definitions m ON m.model_definition_id = e.model_definition_id LEFT JOIN model_artifacts a ON a.model_definition_id = e.model_definition_id AND a.promotion_status IN ('active', 'approved') ORDER BY e.created_at DESC LIMIT 1000")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]metricView, 0)
	for rows.Next() {
		var item metricView
		var low, high sql.NullFloat64
		if err := rows.Scan(&item.RunID, &item.ModelDefinitionID, &item.ModelKey, &item.ModelVersion, &item.Scope, &item.SeasonID, &item.Round, &item.Market, &item.Metric, &item.Value, &item.SampleCount, &low, &high, &item.PromotionStatus); err != nil {
			return nil, err
		}
		item.Source = source
		if low.Valid {
			item.ConfidenceLow = &low.Float64
		}
		if high.Valid {
			item.ConfidenceHigh = &high.Float64
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) loadForwardSummary(r *http.Request) (forwardSummary, error) {
	var result forwardSummary
	var logLoss, brier, accuracy sql.NullFloat64
	query := `SELECT count(*) FILTER (WHERE pr.eligible),
       count(*) FILTER (WHERE pr.eligible AND settlement.prediction_settlement_id IS NOT NULL),
       count(*) FILTER (WHERE pr.eligible AND settlement.prediction_settlement_id IS NULL),
       (SELECT avg(log_loss) FROM prediction_settlements WHERE settlement_track = 'forward_observed'),
       (SELECT avg(brier_score) FROM prediction_settlements WHERE settlement_track = 'forward_observed'),
       (SELECT avg((correct)::int::double precision) FROM prediction_settlements WHERE settlement_track = 'forward_observed')
FROM prediction_revisions pr
JOIN prediction_runs run ON run.prediction_run_id = pr.prediction_run_id
LEFT JOIN LATERAL (
    SELECT prediction_settlement_id
    FROM prediction_settlements ps
    WHERE ps.prediction_revision_id = pr.prediction_revision_id
      AND ps.settlement_track = 'forward_observed'
    ORDER BY correction_version DESC LIMIT 1
) settlement ON TRUE
WHERE run.run_type = 'frozen'`
	var pending int
	if err := s.DB.QueryRowContext(r.Context(), query).Scan(&result.EligiblePredictions, &result.SettledFixtures, &pending, &logLoss, &brier, &accuracy); err != nil {
		return result, err
	}
	result.PendingFixtures = pending
	result.HasGenuineData = result.SettledFixtures > 0
	result.Provenance = "forward_observed settlement track only"
	if logLoss.Valid {
		result.LogLoss = &logLoss.Float64
	}
	if brier.Valid {
		result.BrierScore = &brier.Float64
	}
	if accuracy.Valid {
		result.Accuracy = &accuracy.Float64
	}
	return result, nil
}

func (s *Server) loadActiveModel(r *http.Request) (map[string]any, error) {
	var id, key, version, status string
	var created time.Time
	var selectedAt time.Time
	var selectedBy, reason string
	err := s.DB.QueryRowContext(r.Context(), `
SELECT a.model_artifact_id::text, m.model_key, m.version, a.promotion_status,
       a.created_at, p.selected_at, p.selected_by, p.reason
FROM production_model_selection p
JOIN model_artifacts a ON a.model_artifact_id = p.model_artifact_id
JOIN model_definitions m ON m.model_definition_id = a.model_definition_id
WHERE p.selection_key = 'production'`).Scan(&id, &key, &version, &status, &created, &selectedAt, &selectedBy, &reason)
	if err == nil {
		return map[string]any{
			"artifact_id": id, "model_key": key, "version": version,
			"promotion_status": status, "created_at": created.UTC(),
			"selected_at": selectedAt.UTC(), "selected_by": selectedBy,
			"selection_reason": reason,
		}, nil
	}
	if err != sql.ErrNoRows && !strings.Contains(strings.ToLower(err.Error()), "production_model_selection") {
		return nil, err
	}
	err = s.DB.QueryRowContext(r.Context(), "SELECT a.model_artifact_id::text, m.model_key, m.version, a.promotion_status, a.created_at FROM model_artifacts a JOIN model_definitions m ON m.model_definition_id = a.model_definition_id WHERE a.promotion_status = 'active' ORDER BY a.created_at DESC LIMIT 1").Scan(&id, &key, &version, &status, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"artifact_id": id, "model_key": key, "version": version, "promotion_status": status, "created_at": created.UTC()}, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func nullableTimeJSON(value sql.NullTime) any {
	if result := nullableTime(value); result != nil {
		return result
	}
	return nil
}

func mostLikelyOutcome(value probabilityView) string {
	if value.Draw >= value.Home && value.Draw >= value.Away {
		return "draw"
	}
	if value.Home >= value.Away {
		return "home"
	}
	return "away"
}

func validPredictionStatus(value string) bool {
	switch value {
	case "", "pending", "provisional", "frozen", "settled", "stale", "unavailable", "missed_cutoff":
		return true
	default:
		return false
	}
}

func isSeasonID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func writeServerError(w http.ResponseWriter, _ string) {
	writeError(w, http.StatusInternalServerError, "internal server error")
}
