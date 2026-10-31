package autobet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	primaryKey      = "primary"
	marketKey       = "over_1_5"
	runtimeState    = "disabled"
	minimumOdds     = 1.20
	selectionRule   = "highest_ranked_frozen_prediction"
	stakingStrategy = "odds_aware_martingale_settlement_gated"
)

type Service struct {
	DB             *sql.DB
	Now            func() time.Time
	CredentialsKey string
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
	Enabled               bool       `json:"enabled"`
	BaseStake             float64    `json:"base_stake"`
	ProfitTarget          float64    `json:"profit_target"`
	StakingStrategy       string     `json:"staking_strategy"`
	CredentialsConfigured bool       `json:"credentials_configured"`
	MaskedUsername        string     `json:"masked_username,omitempty"`
	CredentialsUpdatedAt  *time.Time `json:"credentials_updated_at,omitempty"`
	UsernameCiphertext    string     `json:"-"`
	PasswordCiphertext    string     `json:"-"`
	UpdatedAt             time.Time  `json:"updated_at"`
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
	UnrecoveredLoss     float64 `json:"unrecovered_loss"`
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
	MatchID        int64      `json:"match_id"`
	Status         string     `json:"status"`
	HomeTeam       string     `json:"home_team"`
	AwayTeam       string     `json:"away_team"`
	StakeAmount    float64    `json:"stake_amount"`
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
}

type BetTask struct {
	Candidate *Candidate    `json:"candidate"`
	Selection *BetSelection `json:"selection,omitempty"`
}

type DashboardState struct {
	Settings       Settings       `json:"settings"`
	Runtime        Runtime        `json:"runtime"`
	CurrentSeason  string         `json:"current_season_id,omitempty"`
	SeasonSummary  *SeasonSummary `json:"season_summary,omitempty"`
	NextCandidates []Candidate    `json:"next_candidates,omitempty"`
	// NextCandidate remains populated for older dashboard clients. NextCandidates
	// contains at most the single top-ranked prediction for the next round.
	NextCandidate *Candidate    `json:"next_candidate,omitempty"`
	RecentRounds  []RoundRecord `json:"recent_rounds"`
}

type WorkerTask struct {
	Status        string         `json:"status"`
	CurrentSeason string         `json:"current_season_id,omitempty"`
	Settings      Settings       `json:"settings"`
	Runtime       Runtime        `json:"runtime"`
	SeasonSummary *SeasonSummary `json:"season_summary,omitempty"`
	Bets          []BetTask      `json:"bets,omitempty"`
	// Prepared is a preview of the next rank-one frozen bet while the current
	// bet is awaiting settlement. It is never executable until Status is ready.
	Prepared *BetTask `json:"prepared,omitempty"`
	// Bets contains at most one entry: the highest-ranked frozen prediction for
	// the selected round. Candidate and Selection remain rollout-compatible
	// aliases for that entry.
	Candidate *Candidate    `json:"candidate,omitempty"`
	Selection *BetSelection `json:"selection,omitempty"`
}

// BetSelection contains the latest executable provider quote for the fixed
// Over 1.5 market. It is returned only on the internal worker task response;
// dashboard responses use DashboardState and therefore never expose it.
type BetSelection struct {
	MatchID              int64     `json:"match_id"`
	SeasonYear           string    `json:"season_year"`
	HomeTeamUID          int64     `json:"home_team_uid"`
	HomeTeamProviderID   string    `json:"home_team_provider_id"`
	HomeTeamAbbreviation string    `json:"home_team_abbreviation"`
	AwayTeamUID          int64     `json:"away_team_uid"`
	AwayTeamProviderID   string    `json:"away_team_provider_id"`
	AwayTeamAbbreviation string    `json:"away_team_abbreviation"`
	MarketID             int64     `json:"market_id"`
	MarketName           string    `json:"market_name"`
	Specifiers           string    `json:"specifiers"`
	OutcomeID            string    `json:"outcome_id"`
	OutcomeName          string    `json:"outcome_name"`
	RawOdds              string    `json:"raw_odds"`
	DecimalOdds          float64   `json:"decimal_odds"`
	MarketStatus         int       `json:"market_status"`
	OutcomeActive        int       `json:"outcome_active"`
	SourceTimestampAt    time.Time `json:"source_timestamp_at"`
	ObservedAt           time.Time `json:"observed_at"`
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
	Implied         probability `json:"implied"`
	ModelDifference probability `json:"model_difference"`
}

type goalMarketProbability struct {
	Over25  float64 `json:"over_2_5"`
	Under25 float64 `json:"under_2_5"`
}

type bookmakerGoalMarketPayload struct {
	MarketID            int64                 `json:"market_id"`
	Specifiers          string                `json:"specifiers"`
	OverOdds            float64               `json:"over_odds"`
	UnderOdds           float64               `json:"under_odds"`
	Implied             goalMarketProbability `json:"implied"`
	ModelDifference     float64               `json:"model_difference"`
	Overround           float64               `json:"overround"`
	PointInTimeVerified bool                  `json:"point_in_time_verified"`
	ObservationKind     string                `json:"observation_kind"`
}

type secondaryGoalPayload struct {
	Over25           float64 `json:"over_2_5"`
	Under25          float64 `json:"under_2_5"`
	BothTeamsToScore float64 `json:"both_teams_to_score"`
	NeitherTeamScore float64 `json:"neither_team_scores"`
}

type predictionPayload struct {
	Bookmaker           *bookmakerPayload           `json:"bookmaker"`
	BookmakerGoalMarket *bookmakerGoalMarketPayload `json:"bookmaker_goal_market"`
	Secondary           *secondaryGoalPayload       `json:"secondary"`
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
	BookmakerGoalMarket  *bookmakerGoalMarketPayload
	Secondary            *secondaryGoalPayload
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

func applyPredictionPayload(item *predictionRow, payload []byte) {
	if item == nil || len(payload) == 0 {
		return
	}
	var decoded predictionPayload
	if json.Unmarshal(payload, &decoded) != nil {
		return
	}
	if decoded.Bookmaker != nil {
		item.Bookmaker = decoded.Bookmaker
		if hasFiniteImplied(decoded.Bookmaker.Implied) {
			item.Bookmaker.ModelDifference = probability{
				Home: item.Probability.Home - decoded.Bookmaker.Implied.Home,
				Draw: item.Probability.Draw - decoded.Bookmaker.Implied.Draw,
				Away: item.Probability.Away - decoded.Bookmaker.Implied.Away,
			}
		}
	}
	item.Secondary = decoded.Secondary
	if decoded.BookmakerGoalMarket != nil {
		item.BookmakerGoalMarket = decoded.BookmakerGoalMarket
		if item.Secondary != nil && isFinite(item.Secondary.Over25) && isFinite(item.BookmakerGoalMarket.Implied.Over25) {
			item.BookmakerGoalMarket.ModelDifference = item.Secondary.Over25 - item.BookmakerGoalMarket.Implied.Over25
		}
	}
}

func hasFiniteImplied(value probability) bool {
	return isFinite(value.Home) && isFinite(value.Draw) && isFinite(value.Away)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
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
		Settings:       settings,
		Runtime:        runtime,
		CurrentSeason:  seasonID,
		NextCandidates: make([]Candidate, 0),
		RecentRounds:   make([]RoundRecord, 0),
	}
	if seasonID != "" {
		summary, err := s.loadSeasonSummary(ctx, seasonID, settings)
		if err != nil {
			return DashboardState{}, err
		}
		state.SeasonSummary = &summary
		candidates, err := s.nextCandidates(ctx, seasonID, settings, summary)
		if err != nil {
			return DashboardState{}, err
		}
		state.NextCandidates = candidates
		if len(state.NextCandidates) > 0 {
			selection, selectionErr := s.loadBetSelection(ctx, state.NextCandidates[0].MatchID)
			if selectionErr != nil {
				return DashboardState{}, selectionErr
			}
			if selection != nil {
				if stake, stakeErr := determineMartingaleStake(settings.BaseStake, summary.UnrecoveredLoss, summary.ActivePendingStake, selection.DecimalOdds); stakeErr == nil {
					state.NextCandidates[0].StakeAmount = stake
					summary.NextStake = stake
				}
			}
		}
		if len(candidates) > 0 {
			state.NextCandidate = &state.NextCandidates[0]
		}
	}
	state.RecentRounds, err = s.loadRecentRounds(ctx, 24)
	if err != nil {
		return DashboardState{}, err
	}
	return state, nil
}

func (s *Service) UpdateSettings(ctx context.Context, accountID string, enabled bool, baseStake, profitTarget float64) error {
	if err := s.ensureRows(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(accountID) == "" {
		accountID = "primary"
	}
	if baseStake <= 0 {
		return errors.New("base stake must be positive")
	}
	if profitTarget <= 0 {
		return errors.New("profit target must be positive")
	}
	_, err := s.DB.ExecContext(ctx, `
UPDATE auto_betting_settings
SET enabled = $2,
    base_stake = $3,
    profit_target = $4,
    updated_by = NULLIF($5, ''),
    updated_at = $6
WHERE setting_key = $1`, primaryKey, enabled, baseStake, profitTarget, accountID, s.now())
	return err
}

func (s *Service) UpdateCredentials(ctx context.Context, accountID, username, password string) error {
	if err := s.ensureRows(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(accountID) == "" {
		accountID = "primary"
	}
	username = normalizeCredential(username)
	password = normalizeCredential(password)
	if username == "" || password == "" {
		return errors.New("bet9ja username and password are both required")
	}
	key, err := credentialEncryptionKey(s.CredentialsKey)
	if err != nil {
		return err
	}
	encryptedUsername, err := encryptCredentialValue(key, username)
	if err != nil {
		return err
	}
	encryptedPassword, err := encryptCredentialValue(key, password)
	if err != nil {
		return err
	}
	now := s.now()
	_, err = s.DB.ExecContext(ctx, `
UPDATE auto_betting_settings
SET bet9ja_username_ciphertext = $2,
    bet9ja_password_ciphertext = $3,
    bet9ja_username_masked = $4,
    credentials_updated_by = NULLIF($5, ''),
    credentials_updated_at = $6,
    updated_by = NULLIF($5, ''),
    updated_at = $6
WHERE setting_key = $1`, primaryKey, encryptedUsername, encryptedPassword, truncate(maskUsername(username), 128), accountID, now)
	return err
}

func (s *Service) LoadWorkerCredentials(ctx context.Context) (Credentials, error) {
	if err := s.ensureRows(ctx); err != nil {
		return Credentials{}, err
	}
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return Credentials{}, err
	}
	if strings.TrimSpace(settings.UsernameCiphertext) == "" || strings.TrimSpace(settings.PasswordCiphertext) == "" {
		return Credentials{}, errors.New("bet9ja credentials are not configured")
	}
	key, err := credentialEncryptionKey(s.CredentialsKey)
	if err != nil {
		return Credentials{}, err
	}
	username, err := decryptCredentialValue(key, settings.UsernameCiphertext)
	if err != nil {
		return Credentials{}, err
	}
	password, err := decryptCredentialValue(key, settings.PasswordCiphertext)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Username: username, Password: password}, nil
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
		Bets:          make([]BetTask, 0),
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
		prepared, prepareErr := s.prepareNextBet(ctx, seasonID, settings, summary)
		if prepareErr != nil {
			return nil, prepareErr
		}
		task.Prepared = prepared
		task.Status = "awaiting_settlement"
		return task, nil
	}
	candidates, err := s.nextCandidates(ctx, seasonID, settings, summary)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return task, nil
	}
	candidate := candidates[0]
	selection, selectionErr := s.loadBetSelection(ctx, candidate.MatchID)
	if selectionErr != nil {
		return nil, selectionErr
	}
	candidateCopy := candidate
	if selection != nil && selection.DecimalOdds >= minimumOdds {
		stake, stakeErr := determineMartingaleStake(settings.BaseStake, summary.UnrecoveredLoss, summary.ActivePendingStake, selection.DecimalOdds)
		if stakeErr != nil {
			// An invalid quote or ledger exposure cannot produce an executable
			// martingale stake for this task.
			task.Status = "idle"
			return task, nil
		}
		candidateCopy.StakeAmount = stake
		summary.NextStake = stake
	} else if selection != nil {
		// The worker records this as a skipped round when the quote is below
		// the configured minimum. No stake is submitted.
		candidateCopy.StakeAmount = summary.NextStake
	}
	task.Bets = append(task.Bets, BetTask{Candidate: &candidateCopy, Selection: selection})
	task.Candidate = task.Bets[0].Candidate
	task.Selection = task.Bets[0].Selection
	task.Status = "ready"
	return task, nil
}

func (s *Service) prepareNextBet(ctx context.Context, seasonID string, settings Settings, summary SeasonSummary) (*BetTask, error) {
	candidates, err := s.nextCandidatesPreview(ctx, seasonID, settings, summary)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	candidateCopy := candidates[0]
	// The result is not known yet, so there is intentionally no projected
	// martingale stake in the preview.
	candidateCopy.StakeAmount = 0
	selection, err := s.loadBetSelection(ctx, candidateCopy.MatchID)
	if err != nil {
		return nil, err
	}
	return &BetTask{Candidate: &candidateCopy, Selection: selection}, nil
}

func (s *Service) loadBetSelection(ctx context.Context, matchID int64) (*BetSelection, error) {
	if matchID <= 0 {
		return nil, errors.New("invalid betting match id")
	}
	var selection BetSelection
	err := s.DB.QueryRowContext(ctx, `
SELECT f.match_id,
       COALESCE(s.provider_year, ''),
       f.home_team_uid, CONCAT('sr:competitor:', f.home_team_uid), COALESCE(ht.abbreviation, ''),
       f.away_team_uid, CONCAT('sr:competitor:', f.away_team_uid), COALESCE(at.abbreviation, ''),
       o.provider_market_id, md.market_name, o.specifiers, o.outcome_id,
       o.outcome_name, o.raw_odds, o.decimal_odds::double precision,
       o.market_status, o.outcome_active, o.source_timestamp_at, o.last_observed_at
FROM fixtures f
JOIN seasons s ON s.season_id = f.season_id
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
JOIN odds_snapshots o ON o.match_id = f.match_id
JOIN market_definitions md ON md.market_definition_id = o.market_definition_id
WHERE f.match_id = $1
  AND o.provider_market_id = 18
  AND o.specifiers = 'total=1.5'
  AND o.outcome_id = '12'
  AND o.odds_valid = true
  AND o.decimal_odds IS NOT NULL
  AND o.market_status = 1
  AND o.outcome_active = 1
  AND o.observation_kind = 'live_observation'
ORDER BY o.last_observed_at DESC, o.source_timestamp_at DESC, o.odds_snapshot_id DESC
LIMIT 1`, matchID).Scan(
		&selection.MatchID,
		&selection.SeasonYear,
		&selection.HomeTeamUID, &selection.HomeTeamProviderID, &selection.HomeTeamAbbreviation,
		&selection.AwayTeamUID, &selection.AwayTeamProviderID, &selection.AwayTeamAbbreviation,
		&selection.MarketID, &selection.MarketName, &selection.Specifiers, &selection.OutcomeID,
		&selection.OutcomeName, &selection.RawOdds, &selection.DecimalOdds,
		&selection.MarketStatus, &selection.OutcomeActive, &selection.SourceTimestampAt, &selection.ObservedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load current betting selection: %w", err)
	}
	selection.SourceTimestampAt = selection.SourceTimestampAt.UTC()
	selection.ObservedAt = selection.ObservedAt.UTC()
	return &selection, nil
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
    last_login_at = COALESCE($9, auto_betting_runtime.last_login_at),
    last_error = NULLIF($10, ''),
    updated_at = $11
WHERE runtime_key = $1`, primaryKey, state, truncate(update.WorkerID, 128), update.CurrentSeasonID, update.CurrentRound, update.CurrentMatchID, truncate(update.CurrentURL, 512), heartbeat, nullableTimestamp(loginAt), truncate(update.LastError, 8192), s.now())
	return err
}

func (s *Service) RecordSkip(ctx context.Context, seasonID string, round int, matchID int64, reason string, observedOdds *float64) error {
	_, _, candidate, err := s.validateCandidateAction(ctx, seasonID, round, matchID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"decision_basis":         candidate.DecisionBasis,
		"decision_outcome":       candidate.DecisionOutcome,
		"decision_probability":   candidate.DecisionProbability,
		"point_in_time_verified": candidate.PointInTimeVerified,
		"selection_rule":         selectionRule,
		"staking_strategy":       stakingStrategy,
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
    $1, $2, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, 'skipped', 0,
    $10, NULLIF($11, 0), $12, $13::jsonb, $14
)`, candidate.SeasonID, candidate.Round, candidate.PredictionRunID, candidate.PredictionRevisionID, candidate.MatchID,
		candidate.HomeTeam, candidate.AwayTeam, candidate.DecisionRank, marketKey, minimumOdds, derefFloat(observedOdds), truncate(strings.TrimSpace(reason), 256), string(encoded), s.now())
	return err
}

func (s *Service) RecordPlaced(ctx context.Context, seasonID string, round int, matchID int64, quotedOdds, placedStake float64, betReference string) error {
	settings, summary, candidate, err := s.validateCandidateAction(ctx, seasonID, round, matchID)
	if err != nil {
		return err
	}
	if !isFinite(quotedOdds) || quotedOdds < minimumOdds {
		return errors.New("quoted odds must be at least 1.20 for martingale staking")
	}
	stake := placedStake
	if !isFinite(stake) || stake < 0 {
		return errors.New("placed stake must be non-negative")
	}
	if stake == 0 {
		stake, err = determineMartingaleStake(settings.BaseStake, summary.UnrecoveredLoss, summary.ActivePendingStake, quotedOdds)
		if err != nil {
			return err
		}
	} else {
		stake = roundMoney(stake)
		if stake <= 0 {
			return errors.New("placed stake must be positive")
		}
	}
	payload := map[string]any{
		"decision_basis":                  candidate.DecisionBasis,
		"decision_outcome":                candidate.DecisionOutcome,
		"decision_probability":            candidate.DecisionProbability,
		"point_in_time_verified":          candidate.PointInTimeVerified,
		"selection_rule":                  selectionRule,
		"staking_strategy":                stakingStrategy,
		"base_stake":                      settings.BaseStake,
		"settled_profit_before_bet":       summary.TotalProfit,
		"unrecovered_loss_before_bet":     summary.UnrecoveredLoss,
		"active_pending_stake_before_bet": summary.ActivePendingStake,
		"placed_stake":                    stake,
	}
	if candidate.DecisionMarketDifference != nil {
		payload["decision_market_difference"] = *candidate.DecisionMarketDifference
	}
	encoded, _ := json.Marshal(payload)
	potential := roundMoney(stake * quotedOdds)
	_, err = s.DB.ExecContext(ctx, `
INSERT INTO auto_betting_rounds (
    season_id, round_no, prediction_run_id, prediction_revision_id, match_id,
    home_team, away_team, decision_rank, market_key, status, stake_amount,
    minimum_odds, quoted_odds, potential_return, bet_reference, decision_payload,
    placed_at, updated_at
) VALUES (
    $1, $2, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, 'placed', $10,
    $11, $12, $13, NULLIF($14, ''), $15::jsonb, $16, $16
)`, candidate.SeasonID, candidate.Round, candidate.PredictionRunID, candidate.PredictionRevisionID, candidate.MatchID,
		candidate.HomeTeam, candidate.AwayTeam, candidate.DecisionRank, marketKey, stake, minimumOdds, quotedOdds, potential, truncate(strings.TrimSpace(betReference), 256), string(encoded), s.now())
	return err
}

func (s *Service) Settle(ctx context.Context) (SettlementReport, error) {
	if err := s.ensureRows(ctx); err != nil {
		return SettlementReport{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT r.auto_betting_round_id::text, r.season_id, r.round_no, r.stake_amount, r.quoted_odds,
       f.status, f.result_home, f.result_away,
       f.half_time_home, f.half_time_away, f.full_time_home, f.full_time_away,
       f.in_live_score
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
		var homeScore, awayScore, halfTimeHome, halfTimeAway, fullTimeHome, fullTimeAway sql.NullInt64
		var inLiveScore bool
		if err := rows.Scan(&id, &seasonID, &round, &stake, &odds, &fixtureStatus, &homeScore, &awayScore,
			&halfTimeHome, &halfTimeAway, &fullTimeHome, &fullTimeAway, &inLiveScore); err != nil {
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
		outcome, score, settled := over15SettlementDecision(
			fixtureStatus,
			inLiveScore,
			nullableMatchScore(homeScore, awayScore),
			nullableMatchScore(halfTimeHome, halfTimeAway),
			nullableMatchScore(fullTimeHome, fullTimeAway),
		)
		if !settled || score == nil {
			continue
		}
		home := score.Home
		away := score.Away
		if outcome == "won" {
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

type matchScore struct {
	Home int
	Away int
}

func nullableMatchScore(home, away sql.NullInt64) *matchScore {
	if !home.Valid || !away.Valid {
		return nil
	}
	return &matchScore{Home: int(home.Int64), Away: int(away.Int64)}
}

func over15SettlementDecision(fixtureStatus string, inLiveScore bool, current, halftime, fulltime *matchScore) (string, *matchScore, bool) {
	if fulltime != nil {
		if fulltime.Home+fulltime.Away > 1 {
			return "won", fulltime, true
		}
		return "lost", fulltime, true
	}
	for _, score := range []*matchScore{current, halftime} {
		if score != nil && score.Home+score.Away > 1 {
			return "won", score, true
		}
	}
	if inLiveScore || !strings.EqualFold(fixtureStatus, "completed") || current == nil {
		return "", nil, false
	}
	return "lost", current, true
}

func (s *Service) validateCandidateAction(ctx context.Context, seasonID string, round int, matchID int64) (Settings, SeasonSummary, *Candidate, error) {
	if strings.TrimSpace(seasonID) == "" || round < 1 || round > 30 || matchID <= 0 {
		return Settings{}, SeasonSummary{}, nil, errors.New("invalid season, round, or match")
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
	if summary.PendingSettlements > 0 {
		return Settings{}, SeasonSummary{}, nil, errors.New("awaiting settlement before the next auto betting decision")
	}
	var exists bool
	if err := s.DB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM auto_betting_rounds
    WHERE season_id = $1 AND round_no = $2
)`, seasonID, round).Scan(&exists); err != nil {
		return Settings{}, SeasonSummary{}, nil, err
	}
	if exists {
		return Settings{}, SeasonSummary{}, nil, errors.New("round already has an auto betting decision")
	}
	candidates, err := s.candidatesForRound(ctx, seasonID, round, summary.NextStake)
	if err != nil {
		return Settings{}, SeasonSummary{}, nil, err
	}
	for index := range candidates {
		if candidates[index].MatchID == matchID {
			return settings, summary, &candidates[index], nil
		}
	}
	return Settings{}, SeasonSummary{}, nil, errors.New("the highest-ranked frozen betting candidate is not available for this match")
}

func (s *Service) nextCandidates(ctx context.Context, seasonID string, settings Settings, summary SeasonSummary) ([]Candidate, error) {
	if !settings.Enabled || summary.Paused || summary.PendingSettlements > 0 {
		return []Candidate{}, nil
	}
	return s.nextCandidatesPreview(ctx, seasonID, settings, summary)
}

func (s *Service) nextCandidatesPreview(ctx context.Context, seasonID string, settings Settings, summary SeasonSummary) ([]Candidate, error) {
	if !settings.Enabled || summary.Paused {
		return []Candidate{}, nil
	}
	round, err := s.nextEligibleRound(ctx, seasonID, summary.NextStake)
	if err != nil || round == 0 {
		return []Candidate{}, err
	}
	return s.candidatesForRound(ctx, seasonID, round, summary.NextStake)
}

func (s *Service) candidatesForRound(ctx context.Context, seasonID string, round int, stake float64) ([]Candidate, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT DISTINCT ON (p.season_id, p.round_no, pr.match_id)
       p.prediction_run_id::text, pr.prediction_revision_id::text, p.season_id, p.round_no,
       pr.match_id, ht.name, at.name, f.scheduled_at, pr.status,
       pr.home_probability::double precision, pr.draw_probability::double precision, pr.away_probability::double precision,
       pr.probability_payload, pr.point_in_time_verified, pr.odds_observed_at
FROM prediction_runs p
JOIN prediction_revisions pr ON pr.prediction_run_id = p.prediction_run_id
JOIN fixtures f ON f.match_id = pr.match_id
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
WHERE p.season_id = $1
  AND p.round_no = $2
  AND p.status = 'frozen'
  AND p.generated_fixture_count = 8
  AND pr.status = 'frozen'
  AND NOT EXISTS (
      SELECT 1
      FROM auto_betting_rounds existing
      WHERE existing.season_id = p.season_id
        AND existing.round_no = p.round_no
  )
ORDER BY p.season_id, p.round_no, pr.match_id,
         p.prediction_as_of DESC, pr.revision_no DESC`, seasonID, round)
	if err != nil {
		return []Candidate{}, err
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
			return []Candidate{}, err
		}
		item.ScheduledAt = item.ScheduledAt.UTC()
		if oddsAt.Valid {
			value := oddsAt.Time.UTC()
			item.OddsObservedAt = &value
		}
		applyPredictionPayload(&item, payload)
		predictions = append(predictions, item)
	}
	if err := rows.Err(); err != nil {
		return []Candidate{}, err
	}
	if len(predictions) == 0 {
		return []Candidate{}, nil
	}
	// Auto betting uses exactly one fixture per frozen round: the first item
	// is the same rank-one decision shown by the Predictions dashboard.
	ranked := topRankedPrediction(rankPredictions(predictions))
	result := make([]Candidate, 0, len(ranked))
	for _, item := range ranked {
		result = append(result, Candidate{
			SeasonID:                 item.SeasonID,
			Round:                    item.Round,
			PredictionRunID:          item.PredictionRunID,
			PredictionRevisionID:     item.PredictionRevisionID,
			MatchID:                  item.MatchID,
			HomeTeam:                 item.HomeTeam,
			AwayTeam:                 item.AwayTeam,
			ScheduledAt:              item.ScheduledAt,
			Status:                   item.Status,
			DecisionRank:             item.DecisionRank,
			DecisionBasis:            item.DecisionBasis,
			DecisionOutcome:          item.DecisionOutcome,
			DecisionProbability:      item.DecisionProbability,
			DecisionMarketDifference: item.DecisionMarketDifference,
			PointInTimeVerified:      item.PointInTimeVerified,
			OddsObservedAt:           item.OddsObservedAt,
			StakeAmount:              stake,
		})
	}
	return result, nil
}

func (s *Service) nextEligibleRound(ctx context.Context, seasonID string, stake float64) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT DISTINCT p.round_no
FROM prediction_runs p
JOIN round_states rs ON rs.season_id = p.season_id AND rs.round_no = p.round_no
WHERE p.season_id = $1
  AND p.status = 'frozen'
  AND p.generated_fixture_count = 8
  AND rs.state NOT IN ('settled', 'missed_cutoff')
  AND rs.earliest_kickoff_at IS NOT NULL
  AND rs.earliest_kickoff_at >= $2
ORDER BY p.round_no`, seasonID, s.now())
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var round int
		if err := rows.Scan(&round); err != nil {
			return 0, err
		}
		candidates, err := s.candidatesForRound(ctx, seasonID, round, stake)
		if err != nil {
			return 0, err
		}
		if len(candidates) > 0 {
			return round, nil
		}
	}
	return 0, rows.Err()
}

func rankPredictions(items []predictionRow) []rankedPrediction {
	ranked := make([]rankedPrediction, 0, len(items))
	for index, item := range items {
		outcome, diff, basis := decisionSignal(item)
		probability := outcomeProbability(item.Probability, item.Secondary, outcome)
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

func topRankedPrediction(items []rankedPrediction) []rankedPrediction {
	if len(items) > 1 {
		return items[:1]
	}
	return items
}

func decisionSignal(item predictionRow) (string, *float64, string) {
	if difference, ok := over25MarketDifference(item); ok && difference > 0 {
		value := difference
		return "over_2_5", &value, "goal_market_difference"
	}
	if item.Secondary != nil && isFinite(item.Secondary.Over25) {
		return "over_2_5", nil, "model_probability"
	}
	return mostLikelyOutcome(item.Probability), nil, "model_probability"
}

func over25MarketDifference(item predictionRow) (float64, bool) {
	if item.BookmakerGoalMarket == nil || !item.BookmakerGoalMarket.PointInTimeVerified || item.Secondary == nil {
		return 0, false
	}
	if !isFinite(item.Secondary.Over25) || !isFinite(item.BookmakerGoalMarket.Implied.Over25) {
		return 0, false
	}
	difference := item.Secondary.Over25 - item.BookmakerGoalMarket.Implied.Over25
	if !isFinite(difference) {
		return 0, false
	}
	return difference, true
}

func marketSortValue(item rankedPrediction) float64 {
	if item.DecisionBasis == "goal_market_difference" && item.DecisionMarketDifference != nil {
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

func outcomeProbability(value probability, secondary *secondaryGoalPayload, outcome string) float64 {
	switch outcome {
	case "over_2_5":
		if secondary != nil {
			return secondary.Over25
		}
		return 0
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
	var maskedUsername, usernameCiphertext, passwordCiphertext sql.NullString
	var credentialsUpdatedAt sql.NullTime
	err := s.DB.QueryRowContext(ctx, `
SELECT enabled, base_stake::double precision, profit_target::double precision,
	       updated_at, COALESCE(bet9ja_username_masked, ''), credentials_updated_at,
       COALESCE(bet9ja_username_ciphertext, ''), COALESCE(bet9ja_password_ciphertext, '')
FROM auto_betting_settings
WHERE setting_key = $1`, primaryKey).Scan(&settings.Enabled, &settings.BaseStake, &settings.ProfitTarget, &settings.UpdatedAt, &maskedUsername, &credentialsUpdatedAt, &usernameCiphertext, &passwordCiphertext)
	if err != nil {
		return Settings{}, err
	}
	settings.UpdatedAt = settings.UpdatedAt.UTC()
	if maskedUsername.Valid {
		settings.MaskedUsername = maskedUsername.String
	}
	if credentialsUpdatedAt.Valid {
		value := credentialsUpdatedAt.Time.UTC()
		settings.CredentialsUpdatedAt = &value
	}
	if usernameCiphertext.Valid {
		settings.UsernameCiphertext = usernameCiphertext.String
	}
	if passwordCiphertext.Valid {
		settings.PasswordCiphertext = passwordCiphertext.String
	}
	settings.CredentialsConfigured = strings.TrimSpace(settings.UsernameCiphertext) != "" && strings.TrimSpace(settings.PasswordCiphertext) != ""
	settings.StakingStrategy = stakingStrategy
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
	var pendingStake float64
	err = s.DB.QueryRowContext(ctx, `
SELECT COALESCE(sum(stake_amount), 0)::double precision
FROM auto_betting_rounds
WHERE season_id = $1
  AND status = 'placed'`, seasonID).Scan(&pendingStake)
	if err != nil {
		return SeasonSummary{}, err
	}
	summary.ActivePendingStake = pendingStake
	unrecoveredLoss, err := s.loadUnrecoveredLoss(ctx, seasonID)
	if err != nil {
		return SeasonSummary{}, err
	}
	summary.UnrecoveredLoss = unrecoveredLoss
	// The exact next stake is quote-dependent. This is the base-stake estimate
	// used until the current Over 1.5 quote is loaded by the worker task.
	summary.NextStake = determineNextStake(settings.BaseStake)
	summary.ProfitTargetReached = summary.TotalProfit >= settings.ProfitTarget
	if summary.ProfitTargetReached {
		summary.Paused = true
		summary.PauseReason = fmt.Sprintf("Season profit target of %.2f naira reached", settings.ProfitTarget)
	}
	return summary, nil
}

func (s *Service) loadUnrecoveredLoss(ctx context.Context, seasonID string) (float64, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT status, stake_amount::double precision, COALESCE(realized_profit, 0)::double precision
FROM auto_betting_rounds
WHERE season_id = $1
  AND status IN ('won', 'lost', 'void')
ORDER BY COALESCE(settled_at, created_at), created_at, auto_betting_round_id`, seasonID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	unrecoveredLoss := 0.0
	for rows.Next() {
		var status string
		var stake, realizedProfit float64
		if err := rows.Scan(&status, &stake, &realizedProfit); err != nil {
			return 0, err
		}
		unrecoveredLoss = applySettledResultToRecovery(unrecoveredLoss, status, stake, realizedProfit)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return roundMoney(unrecoveredLoss), nil
}

func applySettledResultToRecovery(unrecoveredLoss float64, status string, stake, realizedProfit float64) float64 {
	if !isFinite(unrecoveredLoss) || unrecoveredLoss < 0 {
		unrecoveredLoss = 0
	}
	switch status {
	case "lost":
		loss := 0.0
		if isFinite(stake) && stake > 0 {
			loss = stake
		}
		// Prefer the larger value so a provider fee or rounding difference
		// cannot leave part of a losing stake unrecovered.
		if isFinite(realizedProfit) && realizedProfit < 0 && -realizedProfit > loss {
			loss = -realizedProfit
		}
		unrecoveredLoss += loss
	case "won":
		if isFinite(realizedProfit) && realizedProfit > 0 {
			unrecoveredLoss -= realizedProfit
			if unrecoveredLoss < 0 {
				unrecoveredLoss = 0
			}
		}
	}
	return roundMoney(unrecoveredLoss)
}

func (s *Service) loadRecentRounds(ctx context.Context, limit int) ([]RoundRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT season_id, round_no, COALESCE(match_id, 0), status, home_team, away_team, stake_amount::double precision,
       quoted_odds::double precision, potential_return::double precision,
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
		if err := rows.Scan(&item.SeasonID, &item.Round, &item.MatchID, &item.Status, &item.HomeTeam, &item.AwayTeam, &item.StakeAmount,
			&quoted, &potential, &realizedReturn, &realizedProfit, &item.SkipReason, &item.BetReference, &placedAt, &settledAt, &item.CreatedAt); err != nil {
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

func determineNextStake(baseStake float64) float64 {
	// Without a live quote, expose the configured starting stake as the estimate.
	return roundMoney(baseStake)
}

func determineMartingaleStake(baseStake, unrecoveredLoss, activePendingStake, quotedOdds float64) (float64, error) {
	if !isFinite(baseStake) || baseStake <= 0 {
		return 0, errors.New("base stake must be positive for martingale staking")
	}
	if !isFinite(unrecoveredLoss) || unrecoveredLoss < 0 || !isFinite(activePendingStake) || activePendingStake < 0 {
		return 0, errors.New("martingale ledger exposure is invalid")
	}
	if !isFinite(quotedOdds) || quotedOdds < minimumOdds {
		return 0, errors.New("quoted odds must be at least 1.20 for martingale staking")
	}

	// Keep the first wager equal to baseStake while targeting the profit that
	// a base-stake wager at the current odds would have produced.
	profitTarget := baseStake * (quotedOdds - 1)
	stake := (unrecoveredLoss + activePendingStake + profitTarget) / (quotedOdds - 1)
	if !isFinite(stake) || stake <= 0 {
		return 0, errors.New("martingale stake could not be calculated")
	}
	return roundUpMoney(stake), nil
}

func roundUpMoney(value float64) float64 {
	if !isFinite(value) || value <= 0 {
		return 0
	}
	return math.Ceil((value-1e-9)*100) / 100
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
