package autobet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	primaryKey   = "primary"
	marketKey    = "over_1_5"
	runtimeState = "disabled"
)

type Service struct {
	DB  *sql.DB
	Now func() time.Time
}

func NewService(db *sql.DB) *Service {
	return &Service{DB: db, Now: time.Now}
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type Settings struct {
	Enabled      bool      `json:"enabled"`
	BaseStake    float64   `json:"base_stake"`
	ProfitTarget float64   `json:"profit_target"`
	MinimumOdds  float64   `json:"minimum_odds"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Runtime struct {
	State           string     `json:"state"`
	WorkerID        string     `json:"worker_id,omitempty"`
	CurrentSeasonID string     `json:"current_season_id,omitempty"`
	CurrentRound    int        `json:"current_round,omitempty"`
	CurrentMatchID  int64      `json:"current_match_id,omitempty"`
	CurrentURL      string     `json:"current_url,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type SeasonSummary struct {
	SeasonID            string  `json:"season_id,omitempty"`
	TotalStaked         float64 `json:"total_staked"`
	TotalReturned       float64 `json:"total_returned"`
	TotalProfit         float64 `json:"total_profit"`
	BetsPlaced          int     `json:"bets_placed"`
	BetsWon             int     `json:"bets_won"`
	BetsLost            int     `json:"bets_lost"`
	BetsSkipped         int     `json:"bets_skipped"`
	PendingSettlements  int     `json:"pending_settlements"`
	Paused              bool    `json:"paused"`
	PauseReason         string  `json:"pause_reason,omitempty"`
	NextStake           float64 `json:"next_stake"`
	ActivePendingStake  float64 `json:"active_pending_stake"`
	ProfitTarget        float64 `json:"profit_target"`
	ProfitTargetReached bool    `json:"profit_target_reached"`
}

type RoundRecord struct {
	SeasonID       string     `json:"season_id"`
	Round          int        `json:"round"`
	Status         string     `json:"status"`
	HomeTeam       string     `json:"home_team"`
	AwayTeam       string     `json:"away_team"`
	StakeAmount    float64    `json:"stake_amount"`
	MinimumOdds    float64    `json:"minimum_odds"`
	QuotedOdds     *float64   `json:"quoted_odds,omitempty"`
	PotentialRet   *float64   `json:"potential_return,omitempty"`
	RealizedRet    *float64   `json:"realized_return,omitempty"`
	RealizedProfit *float64   `json:"realized_profit,omitempty"`
	SkipReason     string     `json:"skip_reason,omitempty"`
	BetReference   string     `json:"bet_reference,omitempty"`
	PlacedAt       *time.Time `json:"placed_at,omitempty"`
	SettledAt      *time.Time `json:"settled_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Candidate struct {
	SeasonID                 string     `json:"season_id"`
	Round                    int        `json:"round"`
	PredictionRunID          string     `json:"prediction_run_id"`
	PredictionRevisionID     string     `json:"prediction_revision_id"`
	MatchID                  int64      `json:"match_id"`
	HomeTeam                 string     `json:"home_team"`
	AwayTeam                 string     `json:"away_team"`
	ScheduledAt              time.Time  `json:"scheduled_at"`
	Status                   string     `json:"status"`
	DecisionRank             int        `json:"decision_rank"`
	DecisionBasis            string     `json:"decision_basis"`
	DecisionOutcome          string     `json:"decision_outcome"`
	DecisionProbability      float64    `json:"decision_probability"`
	DecisionMarketDifference *float64   `json:"decision_market_difference,omitempty"`
	PointInTimeVerified      bool       `json:"point_in_time_verified"`
	OddsObservedAt           *time.Time `json:"odds_observed_at,omitempty"`
	StakeAmount              float64    `json:"stake_amount"`
	MinimumOdds              float64    `json:"minimum_odds"`
}

type DashboardState struct {
	Settings      Settings       `json:"settings"`
	Runtime       Runtime        `json:"runtime"`
	CurrentSeason string         `json:"current_season_id,omitempty"`
	SeasonSummary *SeasonSummary `json:"season_summary,omitempty"`
	NextCandidate *Candidate     `json:"next_candidate,omitempty"`
	RecentRounds  []RoundRecord  `json:"recent_rounds"`
}

type WorkerTask struct {
	Status        string         `json:"status"`
	CurrentSeason string         `json:"current_season_id,omitempty"`
	Settings      Settings       `json:"settings"`
	Runtime       Runtime        `json:"runtime"`
	SeasonSummary *SeasonSummary `json:"season_summary,omitempty"`
	Candidate     *Candidate     `json:"candidate,omitempty"`
}

type RuntimeUpdate struct {
	State           string
	WorkerID        string
	CurrentSeasonID string
	CurrentRound    int
	CurrentMatchID  int64
	CurrentURL      string
	LastHeartbeatAt *time.Time
	LastLoginAt     *time.Time
	LastError       string
}

type SettlementReport struct {
	SettledCount int     `json:"settled_count"`
	WonCount     int     `json:"won_count"`
	LostCount    int     `json:"lost_count"`
	VoidCount    int     `json:"void_count"`
	TotalProfit  float64 `json:"total_profit"`
}

type probability struct {
	Home float64 `json:"home"`
	Draw float64 `json:"draw"`
	Away float64 `json:"away"`
}

type bookmakerPayload struct {
	ModelDifference probability `json:"model_difference"`
}

type predictionPayload struct {
	Bookmaker *bookmakerPayload `json:"bookmaker"`
}

type predictionRow struct {
	PredictionRunID      string
	PredictionRevisionID string
	SeasonID             string
	Round                int
	MatchID              int64
	HomeTeam             string
	AwayTeam             string
	ScheduledAt          time.Time
	Status               string
	Probability          probability
	PointInTimeVerified  bool
	OddsObservedAt       *time.Time
	Bookmaker            *bookmakerPayload
}

type rankedPrediction struct {
	predictionRow
	DecisionRank             int
	DecisionBasis            string
	DecisionOutcome          string
	DecisionProbability      float64
	DecisionMarketDifference *float64
	originalIndex            int
}

func (s *Service) ensureRows(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return errors.New("auto betting database is nil")
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO auto_betting_settings (setting_key) VALUES ($1) ON CONFLICT (setting_key) DO NOTHING`, primaryKey); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO auto_betting_runtime (runtime_key, state) VALUES ($1, $2) ON CONFLICT (runtime_key) DO NOTHING`, primaryKey, runtimeState)
	return err
}

func (s *Service) LoadDashboard(ctx context.Context) (DashboardState, error) {
	if err := s.ensureRows(ctx); err != nil {
		return DashboardState{}, err
	}
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return DashboardState{}, err
	}
	runtime, err := s.loadRuntime(ctx)
	if err != nil {
		return DashboardState{}, err
	}
	seasonID, err := s.currentSeasonID(ctx)
	if err != nil {
		return DashboardState{}, err
	}
	state := DashboardState{
		Settings:      settings,
		Runtime:       runtime,
		CurrentSeason: seasonID,
		RecentRounds:  make([]RoundRecord, 0),
	}
	if seasonID != "" {
		summary, err := s.loadSeasonSummary(ctx, seasonID, settings)
		if err != nil {
			return DashboardState{}, err
		}
		state.SeasonSummary = &summary
		candidate, err := s.nextCandidate(ctx, seasonID, settings, summary)
		if err != nil {
			return DashboardState{}, err
		}
		state.NextCandidate = candidate
	}
	state.RecentRounds, err = s.loadRecentRounds(ctx, 24)
	if err != nil {
		return DashboardState{}, err
	}
	return state, nil
}

func (s *Service) UpdateSettings(ctx context.Context, accountID string, enabled bool, baseStake float64) error {
	if err := s.ensureRows(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(accountID) == "" {
		accountID = "primary"
	}
	if baseStake <= 0 {
		return errors.New("base stake must be positive")
	}
	_, err := s.DB.ExecContext(ctx, `
UPDATE auto_betting_settings
SET enabled = $2,
    base_stake = $3,
    updated_by = NULLIF($4, ''),
    updated_at = $5
WHERE setting_key = $1`, primaryKey, enabled, baseStake, accountID, s.now())
	return err
}

func (s *Service) NextTask(ctx context.Context) (*WorkerTask, error) {
	if err := s.ensureRows(ctx); err != nil {
		return nil, err
	}
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return nil, err
	}
	runtime, err := s.loadRuntime(ctx)
	if err != nil {
		return nil, err
	}
	seasonID, err := s.currentSeasonID(ctx)
	if err != nil {
		return nil, err
	}
	task := &WorkerTask{
		Status:        "idle",
		CurrentSeason: seasonID,
		Settings:      settings,
		Runtime:       runtime,
	}
	if !settings.Enabled {
		task.Status = "disabled"
		return task, nil
	}
	if seasonID == "" {
		return task, nil
	}
	summary, err := s.loadSeasonSummary(ctx, seasonID, settings)
	if err != nil {
		return nil, err
	}
	task.SeasonSummary = &summary
	if summary.Paused {
		task.Status = "paused_profit_target"
		return task, nil
	}
	if summary.PendingSettlements > 0 {
		task.Status = "awaiting_settlement"
		return task, nil
	}
	candidate, err := s.nextCandidate(ctx, seasonID, settings, summary)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return task, nil
	}
	task.Candidate = candidate
	task.Status = "ready"
	return task, nil
}

func (s *Service) UpdateRuntime(ctx context.Context, update RuntimeUpdate) error {
	if err := s.ensureRows(ctx); err != nil {
		return err
	}
	state := strings.TrimSpace(update.State)
	if state == "" {
		state = runtimeState
	}
	if !validRuntimeState(state) {
		return errors.New("invalid runtime state")
	}
	heartbeat := time.Time{}
	if update.LastHeartbeatAt != nil {
		heartbeat = update.LastHeartbeatAt.UTC()
	} else {
		heartbeat = s.now()
	}
	loginAt := time.Time{}
	if update.LastLoginAt != nil {
		loginAt = update.LastLoginAt.UTC()
	}
	_, err := s.DB.ExecContext(ctx, `
UPDATE auto_betting_runtime
SET state = $2,
    worker_id = NULLIF($3, ''),
    current_season_id = NULLIF($4, ''),
    current_round_no = NULLIF($5, 0),
    current_match_id = NULLIF($6, 0),
    current_url = NULLIF($7, ''),
    last_heartbeat_at = $8,
    last_login_at = $9,
    last_error = NULLIF($10, ''),
    updated_at = $11
WHERE runtime_key = $1`, primaryKey, state, truncate(update.WorkerID, 128), update.CurrentSeasonID, update.CurrentRound, update.CurrentMatchID, truncate(update.CurrentURL, 512), heartbeat, nullableTimestamp(loginAt), truncate(update.LastError, 1024), s.now())
	return err
}

func (s *Service) RecordSkip(ctx context.Context, seasonID string, round int, reason string, observedOdds *float64) error {
	settings, summary, candidate, err := s.validateRoundAction(ctx, seasonID, round)
	if err != nil {
		return err
	}
	if summary.PendingSettlements > 0 {
		return errors.New("cannot skip while a placed bet is awaiting settlement")
	}
	payload := map[string]any{
		"decision_basis":         candidate.DecisionBasis,
		"decision_outcome":       candidate.DecisionOutcome,
		"decision_probability":   candidate.DecisionProbability,
		"minimum_odds":           settings.MinimumOdds,
		"point_in_time_verified": candidate.PointInTimeVerified,
	}
	if candidate.DecisionMarketDifference != nil {
		payload["decision_market_difference"] = *candidate.DecisionMarketDifference
	}
	if observedOdds != nil {
		payload["observed_odds"] = *observedOdds
	}
	encoded, _ := json.Marshal(payload)
	_, err = s.DB.ExecContext(ctx, `
INSERT INTO auto_betting_rounds (
    season_id, round_no, prediction_run_id, prediction_revision_id, match_id,
    home_team, away_team, decision_rank, market_key, status, stake_amount,
    minimum_odds, quoted_odds, skip_reason, decision_payload, updated_at
) VALUES (
    $1, $2, $3::uuid, $4::uuid, $5, $6, $7, 1, $8, 'skipped', 0,
    $9, NULLIF($10, 0), $11, $12::jsonb, $13
)`, candidate.SeasonID, candidate.Round, candidate.PredictionRunID, candidate.PredictionRevisionID, candidate.MatchID,
		candidate.HomeTeam, candidate.AwayTeam, marketKey, settings.MinimumOdds, derefFloat(observedOdds), truncate(strings.TrimSpace(reason), 256), string(encoded), s.now())
	return err
}

func (s *Service) RecordPlaced(ctx context.Context, seasonID string, round int, quotedOdds float64, betReference string) error {
	settings, summary, candidate, err := s.validateRoundAction(ctx, seasonID, round)
	if err != nil {
		return err
	}
	if quotedOdds < settings.MinimumOdds {
		return errors.New("quoted odds are below the configured minimum")
	}
	if summary.PendingSettlements > 0 {
		return errors.New("cannot place while a prior bet is awaiting settlement")
	}
	payload := map[string]any{
		"decision_basis":         candidate.DecisionBasis,
		"decision_outcome":       candidate.DecisionOutcome,
		"decision_probability":   candidate.DecisionProbability,
		"minimum_odds":           settings.MinimumOdds,
		"point_in_time_verified": candidate.PointInTimeVerified,
	}
	if candidate.DecisionMarketDifference != nil {
		payload["decision_market_difference"] = *candidate.DecisionMarketDifference
	}
	encoded, _ := json.Marshal(payload)
	potential := roundMoney(summary.NextStake * quotedOdds)
	_, err = s.DB.ExecContext(ctx, `
INSERT INTO auto_betting_rounds (
    season_id, round_no, prediction_run_id, prediction_revision_id, match_id,
    home_team, away_team, decision_rank, market_key, status, stake_amount,
    minimum_odds, quoted_odds, potential_return, bet_reference, decision_payload,
    placed_at, updated_at
) VALUES (
    $1, $2, $3::uuid, $4::uuid, $5, $6, $7, 1, $8, 'placed', $9,
    $10, $11, $12, NULLIF($13, ''), $14::jsonb, $15, $15
)`, candidate.SeasonID, candidate.Round, candidate.PredictionRunID, candidate.PredictionRevisionID, candidate.MatchID,
		candidate.HomeTeam, candidate.AwayTeam, marketKey, summary.NextStake, settings.MinimumOdds, quotedOdds, potential, truncate(strings.TrimSpace(betReference), 256), string(encoded), s.now())
	return err
}

func (s *Service) Settle(ctx context.Context) (SettlementReport, error) {
	if err := s.ensureRows(ctx); err != nil {
		return SettlementReport{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT r.auto_betting_round_id::text, r.season_id, r.round_no, r.stake_amount, r.quoted_odds,
       f.status, f.result_home, f.result_away
FROM auto_betting_rounds r
JOIN fixtures f ON f.match_id = r.match_id
WHERE r.status = 'placed'
ORDER BY r.season_id, r.round_no`)
	if err != nil {
		return SettlementReport{}, err
	}
	defer rows.Close()
	report := SettlementReport{}
	for rows.Next() {
		var id, seasonID, fixtureStatus string
		var round int
		var stake, odds float64
		var homeScore, awayScore sql.NullInt64
		if err := rows.Scan(&id, &seasonID, &round, &stake, &odds, &fixtureStatus, &homeScore, &awayScore); err != nil {
			return report, err
		}
		if strings.EqualFold(fixtureStatus, "canceled") {
			if err := s.finalizeRound(ctx, id, "void", stake, stake, 0, nil, nil); err != nil {
				return report, err
			}
			report.SettledCount++
			report.VoidCount++
			continue
		}
		if !homeScore.Valid || !awayScore.Valid {
			continue
		}
		home := int(homeScore.Int64)
		away := int(awayScore.Int64)
		if home+away > 1 {
			returnValue := roundMoney(stake * odds)
			profit := roundMoney(returnValue - stake)
			if err := s.finalizeRound(ctx, id, "won", stake, returnValue, profit, &home, &away); err != nil {
				return report, err
			}
			report.SettledCount++
			report.WonCount++
			report.TotalProfit += profit
			continue
		}
		profit := roundMoney(-stake)
		if err := s.finalizeRound(ctx, id, "lost", stake, 0, profit, &home, &away); err != nil {
			return report, err
		}
		report.SettledCount++
		report.LostCount++
		report.TotalProfit += profit
	}
	return report, rows.Err()
}

func (s *Service) finalizeRound(ctx context.Context, id, status string, stake, realizedReturn, profit float64, homeScore, awayScore *int) error {
	var home, away any
	if homeScore != nil {
		home = *homeScore
	}
	if awayScore != nil {
		away = *awayScore
	}
	_, err := s.DB.ExecContext(ctx, `
UPDATE auto_betting_rounds
SET status = $2,
    realized_return = $3,
    realized_profit = $4,
    settled_at = $5,
    result_home_score = $6,
    result_away_score = $7,
    updated_at = $5
WHERE auto_betting_round_id = $1::uuid AND status = 'placed'`, id, status, realizedReturn, profit, s.now(), home, away)
	return err
}

func (s *Service) validateRoundAction(ctx context.Context, seasonID string, round int) (Settings, SeasonSummary, *Candidate, error) {
	if strings.TrimSpace(seasonID) == "" || round < 1 || round > 30 {
		return Settings{}, SeasonSummary{}, nil, errors.New("invalid season or round")
	}
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return Settings{}, SeasonSummary{}, nil, err
	}
	summary, err := s.loadSeasonSummary(ctx, seasonID, settings)
	if err != nil {
		return Settings{}, SeasonSummary{}, nil, err
	}
	if summary.Paused {
		return Settings{}, SeasonSummary{}, nil, errors.New("season is paused after reaching the profit target")
	}
	var exists bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM auto_betting_rounds WHERE season_id = $1 AND round_no = $2)`, seasonID, round).Scan(&exists); err != nil {
		return Settings{}, SeasonSummary{}, nil, err
	}
	if exists {
		return Settings{}, SeasonSummary{}, nil, errors.New("round already has an auto betting decision")
	}
	candidate, err := s.candidateForRound(ctx, seasonID, round, settings, summary)
	if err != nil {
		return Settings{}, SeasonSummary{}, nil, err
	}
	if candidate == nil {
		return Settings{}, SeasonSummary{}, nil, errors.New("no frozen betting candidate is available for this round")
	}
	return settings, summary, candidate, nil
}

func (s *Service) nextCandidate(ctx context.Context, seasonID string, settings Settings, summary SeasonSummary) (*Candidate, error) {
	if !settings.Enabled || summary.Paused || summary.PendingSettlements > 0 {
		return nil, nil
	}
	var runID string
	err := s.DB.QueryRowContext(ctx, `
SELECT run.prediction_run_id::text
FROM prediction_runs run
WHERE run.season_id = $1
  AND run.status = 'frozen'
  AND run.generated_fixture_count = 8
  AND run.earliest_kickoff_at > $2
  AND NOT EXISTS (
      SELECT 1
      FROM auto_betting_rounds r
      WHERE r.season_id = run.season_id AND r.round_no = run.round_no
  )
ORDER BY run.earliest_kickoff_at, run.prediction_as_of DESC
LIMIT 1`, seasonID, s.now()).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.rankedCandidateForRun(ctx, runID, settings, summary.NextStake)
}

func (s *Service) candidateForRound(ctx context.Context, seasonID string, round int, settings Settings, summary SeasonSummary) (*Candidate, error) {
	var runID string
	err := s.DB.QueryRowContext(ctx, `
SELECT prediction_run_id::text
FROM prediction_runs
WHERE season_id = $1
  AND round_no = $2
  AND status = 'frozen'
  AND generated_fixture_count = 8
ORDER BY prediction_as_of DESC
LIMIT 1`, seasonID, round).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.rankedCandidateForRun(ctx, runID, settings, summary.NextStake)
}

func (s *Service) rankedCandidateForRun(ctx context.Context, runID string, settings Settings, stake float64) (*Candidate, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT run.prediction_run_id::text, pr.prediction_revision_id::text, run.season_id, run.round_no,
       pr.match_id, ht.name, at.name, f.scheduled_at, pr.status,
       pr.home_probability::double precision, pr.draw_probability::double precision, pr.away_probability::double precision,
       pr.probability_payload, pr.point_in_time_verified, pr.odds_observed_at
FROM prediction_runs run
JOIN prediction_revisions pr ON pr.prediction_run_id = run.prediction_run_id
JOIN fixtures f ON f.match_id = pr.match_id
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
WHERE run.prediction_run_id = $1::uuid
ORDER BY f.scheduled_at, pr.match_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	predictions := make([]predictionRow, 0, 8)
	for rows.Next() {
		var item predictionRow
		var payload []byte
		var oddsAt sql.NullTime
		if err := rows.Scan(&item.PredictionRunID, &item.PredictionRevisionID, &item.SeasonID, &item.Round,
			&item.MatchID, &item.HomeTeam, &item.AwayTeam, &item.ScheduledAt, &item.Status,
			&item.Probability.Home, &item.Probability.Draw, &item.Probability.Away,
			&payload, &item.PointInTimeVerified, &oddsAt); err != nil {
			return nil, err
		}
		item.ScheduledAt = item.ScheduledAt.UTC()
		if oddsAt.Valid {
			value := oddsAt.Time.UTC()
			item.OddsObservedAt = &value
		}
		if len(payload) > 0 {
			var decoded predictionPayload
			if json.Unmarshal(payload, &decoded) == nil {
				item.Bookmaker = decoded.Bookmaker
			}
		}
		predictions = append(predictions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(predictions) == 0 {
		return nil, nil
	}
	ranked := rankPredictions(predictions)
	best := ranked[0]
	return &Candidate{
		SeasonID:                 best.SeasonID,
		Round:                    best.Round,
		PredictionRunID:          best.PredictionRunID,
		PredictionRevisionID:     best.PredictionRevisionID,
		MatchID:                  best.MatchID,
		HomeTeam:                 best.HomeTeam,
		AwayTeam:                 best.AwayTeam,
		ScheduledAt:              best.ScheduledAt,
		Status:                   best.Status,
		DecisionRank:             best.DecisionRank,
		DecisionBasis:            best.DecisionBasis,
		DecisionOutcome:          best.DecisionOutcome,
		DecisionProbability:      best.DecisionProbability,
		DecisionMarketDifference: best.DecisionMarketDifference,
		PointInTimeVerified:      best.PointInTimeVerified,
		OddsObservedAt:           best.OddsObservedAt,
		StakeAmount:              stake,
		MinimumOdds:              settings.MinimumOdds,
	}, nil
}

func rankPredictions(items []predictionRow) []rankedPrediction {
	ranked := make([]rankedPrediction, 0, len(items))
	for index, item := range items {
		outcome, diff, basis := decisionSignal(item)
		probability := outcomeProbability(item.Probability, outcome)
		ranked = append(ranked, rankedPrediction{
			predictionRow:            item,
			DecisionBasis:            basis,
			DecisionOutcome:          outcome,
			DecisionProbability:      probability,
			DecisionMarketDifference: diff,
			originalIndex:            index,
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		leftMarket := marketSortValue(ranked[i])
		rightMarket := marketSortValue(ranked[j])
		if rightMarket != leftMarket {
			return leftMarket > rightMarket
		}
		if ranked[j].DecisionProbability != ranked[i].DecisionProbability {
			return ranked[i].DecisionProbability > ranked[j].DecisionProbability
		}
		if !ranked[i].ScheduledAt.Equal(ranked[j].ScheduledAt) {
			return ranked[i].ScheduledAt.Before(ranked[j].ScheduledAt)
		}
		return ranked[i].originalIndex < ranked[j].originalIndex
	})
	for index := range ranked {
		ranked[index].DecisionRank = index + 1
	}
	return ranked
}

func decisionSignal(item predictionRow) (string, *float64, string) {
	if item.Bookmaker != nil && item.PointInTimeVerified {
		bestOutcome := ""
		bestDiff := 0.0
		for _, outcome := range []string{"home", "draw", "away"} {
			diff := outcomeDifference(item.Bookmaker.ModelDifference, outcome)
			if diff > 0 && (bestOutcome == "" || diff > bestDiff) {
				bestOutcome = outcome
				bestDiff = diff
			}
		}
		if bestOutcome != "" {
			value := bestDiff
			return bestOutcome, &value, "market_difference"
		}
	}
	outcome := mostLikelyOutcome(item.Probability)
	return outcome, nil, "model_probability"
}

func marketSortValue(item rankedPrediction) float64 {
	if item.DecisionBasis == "market_difference" && item.DecisionMarketDifference != nil {
		return *item.DecisionMarketDifference
	}
	return -1
}

func mostLikelyOutcome(value probability) string {
	if value.Draw >= value.Home && value.Draw >= value.Away {
		return "draw"
	}
	if value.Home >= value.Away {
		return "home"
	}
	return "away"
}

func outcomeProbability(value probability, outcome string) float64 {
	switch outcome {
	case "draw":
		return value.Draw
	case "away":
		return value.Away
	default:
		return value.Home
	}
}

func outcomeDifference(value probability, outcome string) float64 {
	switch outcome {
	case "draw":
		return value.Draw
	case "away":
		return value.Away
	default:
		return value.Home
	}
}

func (s *Service) currentSeasonID(ctx context.Context) (string, error) {
	var seasonID string
	err := s.DB.QueryRowContext(ctx, `
SELECT season_id
FROM seasons
WHERE season_id = COALESCE(
    (SELECT season_id FROM seasons WHERE current_flag = true ORDER BY last_seen_at DESC LIMIT 1),
    (SELECT season_id FROM live_feed_observations WHERE endpoint_family = 'live_season_fixtures' AND parse_status = 'accepted' ORDER BY observed_at DESC LIMIT 1)
)
LIMIT 1`).Scan(&seasonID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return seasonID, err
}

func (s *Service) loadSettings(ctx context.Context) (Settings, error) {
	var settings Settings
	err := s.DB.QueryRowContext(ctx, `
SELECT enabled, base_stake::double precision, profit_target::double precision, minimum_odds::double precision, updated_at
FROM auto_betting_settings
WHERE setting_key = $1`, primaryKey).Scan(&settings.Enabled, &settings.BaseStake, &settings.ProfitTarget, &settings.MinimumOdds, &settings.UpdatedAt)
	if err != nil {
		return Settings{}, err
	}
	settings.UpdatedAt = settings.UpdatedAt.UTC()
	return settings, nil
}

func (s *Service) loadRuntime(ctx context.Context) (Runtime, error) {
	var runtime Runtime
	var heartbeat, loginAt sql.NullTime
	var seasonID, workerID, currentURL, lastError sql.NullString
	var round sql.NullInt64
	var match sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `
SELECT state, worker_id, current_season_id, current_round_no, current_match_id, current_url,
       last_heartbeat_at, last_login_at, last_error, updated_at
FROM auto_betting_runtime
WHERE runtime_key = $1`, primaryKey).Scan(&runtime.State, &workerID, &seasonID, &round, &match, &currentURL, &heartbeat, &loginAt, &lastError, &runtime.UpdatedAt)
	if err != nil {
		return Runtime{}, err
	}
	runtime.UpdatedAt = runtime.UpdatedAt.UTC()
	if workerID.Valid {
		runtime.WorkerID = workerID.String
	}
	if seasonID.Valid {
		runtime.CurrentSeasonID = seasonID.String
	}
	if round.Valid {
		runtime.CurrentRound = int(round.Int64)
	}
	if match.Valid {
		runtime.CurrentMatchID = match.Int64
	}
	if currentURL.Valid {
		runtime.CurrentURL = currentURL.String
	}
	if lastError.Valid {
		runtime.LastError = lastError.String
	}
	if heartbeat.Valid {
		value := heartbeat.Time.UTC()
		runtime.LastHeartbeatAt = &value
	}
	if loginAt.Valid {
		value := loginAt.Time.UTC()
		runtime.LastLoginAt = &value
	}
	return runtime, nil
}

func (s *Service) loadSeasonSummary(ctx context.Context, seasonID string, settings Settings) (SeasonSummary, error) {
	summary := SeasonSummary{SeasonID: seasonID, ProfitTarget: settings.ProfitTarget}
	err := s.DB.QueryRowContext(ctx, `
SELECT
    COALESCE(sum(stake_amount) FILTER (WHERE status IN ('placed', 'won', 'lost', 'void')), 0)::double precision,
    COALESCE(sum(realized_return), 0)::double precision,
    COALESCE(sum(realized_profit), 0)::double precision,
    count(*) FILTER (WHERE status IN ('placed', 'won', 'lost', 'void'))::integer,
    count(*) FILTER (WHERE status = 'won')::integer,
    count(*) FILTER (WHERE status = 'lost')::integer,
    count(*) FILTER (WHERE status = 'skipped')::integer,
    count(*) FILTER (WHERE status = 'placed')::integer
FROM auto_betting_rounds
WHERE season_id = $1`, seasonID).Scan(&summary.TotalStaked, &summary.TotalReturned, &summary.TotalProfit, &summary.BetsPlaced, &summary.BetsWon, &summary.BetsLost, &summary.BetsSkipped, &summary.PendingSettlements)
	if err != nil {
		return SeasonSummary{}, err
	}
	var latestStatus string
	var latestStake float64
	err = s.DB.QueryRowContext(ctx, `
SELECT status, stake_amount::double precision
FROM auto_betting_rounds
WHERE season_id = $1
  AND status IN ('won', 'lost', 'void')
ORDER BY round_no DESC, updated_at DESC
LIMIT 1`, seasonID).Scan(&latestStatus, &latestStake)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SeasonSummary{}, err
	}
	var pendingStake float64
	err = s.DB.QueryRowContext(ctx, `
SELECT COALESCE(max(stake_amount), 0)::double precision
FROM auto_betting_rounds
WHERE season_id = $1 AND status = 'placed'`, seasonID).Scan(&pendingStake)
	if err != nil {
		return SeasonSummary{}, err
	}
	summary.ActivePendingStake = pendingStake
	if summary.PendingSettlements > 0 && pendingStake > 0 {
		summary.NextStake = pendingStake
	} else if latestStatus == "lost" && latestStake > 0 {
		summary.NextStake = roundMoney(latestStake * 2)
	} else {
		summary.NextStake = roundMoney(settings.BaseStake)
	}
	summary.ProfitTargetReached = summary.TotalProfit >= settings.ProfitTarget
	if summary.ProfitTargetReached {
		summary.Paused = true
		summary.PauseReason = fmt.Sprintf("Season profit target of %.2f naira reached", settings.ProfitTarget)
	}
	return summary, nil
}

func (s *Service) loadRecentRounds(ctx context.Context, limit int) ([]RoundRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT season_id, round_no, status, home_team, away_team, stake_amount::double precision,
       minimum_odds::double precision, quoted_odds::double precision, potential_return::double precision,
       realized_return::double precision, realized_profit::double precision, COALESCE(skip_reason, ''),
       COALESCE(bet_reference, ''), placed_at, settled_at, created_at
FROM auto_betting_rounds
ORDER BY season_id DESC, round_no DESC, created_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RoundRecord, 0, limit)
	for rows.Next() {
		var item RoundRecord
		var quoted, potential, realizedReturn, realizedProfit sql.NullFloat64
		var placedAt, settledAt sql.NullTime
		if err := rows.Scan(&item.SeasonID, &item.Round, &item.Status, &item.HomeTeam, &item.AwayTeam, &item.StakeAmount,
			&item.MinimumOdds, &quoted, &potential, &realizedReturn, &realizedProfit, &item.SkipReason, &item.BetReference, &placedAt, &settledAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		if quoted.Valid {
			value := quoted.Float64
			item.QuotedOdds = &value
		}
		if potential.Valid {
			value := potential.Float64
			item.PotentialRet = &value
		}
		if realizedReturn.Valid {
			value := realizedReturn.Float64
			item.RealizedRet = &value
		}
		if realizedProfit.Valid {
			value := realizedProfit.Float64
			item.RealizedProfit = &value
		}
		if placedAt.Valid {
			value := placedAt.Time.UTC()
			item.PlacedAt = &value
		}
		if settledAt.Valid {
			value := settledAt.Time.UTC()
			item.SettledAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func validRuntimeState(value string) bool {
	switch value {
	case "disabled", "idle", "ready", "placing", "awaiting_settlement", "paused_profit_target", "login_required", "error":
		return true
	default:
		return false
	}
}

func roundMoney(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func nullableTimestamp(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
