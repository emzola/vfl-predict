export type Fixture = {
  match_id: number;
  season_id: string;
  round: number;
  scheduled_at: string;
  home_team_uid: number;
  home_team: string;
  away_team_uid: number;
  away_team: string;
  status: string;
  home_score?: number;
  away_score?: number;
};

export type Probability = { home: number; draw: number; away: number };

export type Bookmaker = {
  home_odds: number;
  draw_odds: number;
  away_odds: number;
  implied: Probability;
  model_difference: Probability;
  overround: number;
};

export type BookmakerGoalMarket = {
  market_id: number;
  specifiers: string;
  over_odds: number;
  under_odds: number;
  implied: { over_2_5: number; under_2_5: number };
  model_difference: number;
  overround: number;
  point_in_time_verified: boolean;
  observation_kind?: string;
};

export type SecondaryGoalPrediction = {
  probability?: Probability;
  lambda_home?: number;
  lambda_away?: number;
  over_2_5?: number;
  under_2_5?: number;
  both_teams_to_score?: number;
  neither_team_scores?: number;
  correct_scores?: Record<string, number>;
  correct_score_residual_tail?: number;
  dynamic_team_strength?: DynamicTeamStrength;
};

export type DynamicTeamStrength = {
  home_rating: number;
  away_rating: number;
  home_rating_uncertainty: number;
  away_rating_uncertainty: number;
  home_attack_strength: number;
  away_attack_strength: number;
  home_defense_strength: number;
  away_defense_strength: number;
  home_attack_uncertainty: number;
  away_attack_uncertainty: number;
  home_defense_uncertainty: number;
  away_defense_uncertainty: number;
  expected_home_goals: number;
  expected_away_goals: number;
  league_scoring_intercept: number;
  home_advantage: number;
  season_matches_used: number;
  total_matches_used: number;
  state_as_of?: string;
};

export type Prediction = {
  prediction_run_id: string;
  prediction_revision_id: string;
  match_id: number;
  season_id: string;
  round: number;
  fixture: Fixture;
  probability: Probability;
  most_likely_outcome: string;
  model_version: string;
  engine?: string;
  engine_version?: string;
  prediction_policy?: string;
  status: string;
  prediction_as_of: string;
  cutoff_at: string;
  earliest_kickoff_at: string;
  odds_observed_at?: string;
  provider_odds_timestamp_at?: string;
  point_in_time_verified: boolean;
  eligible: boolean;
  ineligible_reason?: string;
  created_at: string;
  bookmaker?: Bookmaker;
  bookmaker_goal_market?: BookmakerGoalMarket;
  secondary?: SecondaryGoalPrediction;
  secondary_model?: string;
  dynamic_team_strength?: DynamicTeamStrength;
  feature_values?: Record<string, number>;
  actual_outcome?: string;
  correct?: boolean;
  settlement_track?: string;
};

export type PredictionPage = {
  season_id?: string;
  round?: number;
  status?: string;
  expected_fixture_count: number;
  generated_fixture_count: number;
  model_version?: string;
  prediction_as_of?: string;
  cutoff_at?: string;
  fixtures: Prediction[];
  complete: boolean;
  failure_reason?: string;
  provenance: string;
};

export type PredictionGroup = {
  season_id: string;
  round: number;
  status: string;
  model_version: string;
  prediction_as_of?: string;
  cutoff_at?: string;
  fixtures: Prediction[];
  complete: boolean;
  provenance: string;
};

export type Overview = {
  checked_at: string;
  current_season?: {
    season_id: string;
    provider_season_id: string;
    provider_year?: string;
    current: boolean;
  };
  current_round?: {
    season_id: string;
    round: number;
    state: string;
    fixture_count: number;
    completed_count: number;
    earliest_kickoff_at?: string;
    betting_cutoff_at?: string;
  };
  upcoming_fixtures: Fixture[];
  complete_frozen_rounds: number;
  latest_settled_round?: { round: number; state: string };
  feed_freshness: Record<string, unknown>;
  worker_status: Array<Record<string, unknown>>;
  active_model?: Record<string, unknown>;
  forward_testing: {
    eligible_predictions: number;
    settled_fixtures: number;
    pending_fixtures: number;
    log_loss?: number;
    brier_score?: number;
    accuracy?: number;
    has_genuine_data: boolean;
    provenance: string;
  };
};

export type Season = {
  season_id: string;
  provider_season_id: string;
  provider_year?: string;
  abbreviation?: string;
  current: boolean;
  starts_at?: string;
  ends_at?: string;
};

export type Metric = {
  source: string;
  run_id: string;
  model_definition_id?: number;
  model_key?: string;
  model_version?: string;
  scope?: string;
  season_id?: string;
  round?: number;
  market?: string;
  metric: string;
  value: number;
  sample_count: number;
  promotion_status?: string;
};

export type AutoBettingSettings = {
  enabled: boolean;
  base_stake: number;
  profit_target: number;
  staking_strategy: string;
  credentials_configured: boolean;
  masked_username?: string;
  credentials_updated_at?: string;
  updated_at: string;
};

export type AutoBettingRuntime = {
  state: string;
  worker_id?: string;
  current_season_id?: string;
  current_round?: number;
  current_match_id?: number;
  current_url?: string;
  last_heartbeat_at?: string;
  last_login_at?: string;
  last_error?: string;
  updated_at: string;
};

export type AutoBettingSeasonSummary = {
  season_id?: string;
  total_staked: number;
  total_returned: number;
  total_profit: number;
  bets_placed: number;
  bets_won: number;
  bets_lost: number;
  bets_skipped: number;
  pending_settlements: number;
  paused: boolean;
  pause_reason?: string;
  next_stake: number;
  active_pending_stake: number;
  profit_target: number;
  profit_target_reached: boolean;
};

export type AutoBettingCandidate = {
  season_id: string;
  round: number;
  prediction_run_id: string;
  prediction_revision_id: string;
  match_id: number;
  home_team: string;
  away_team: string;
  scheduled_at: string;
  status: string;
  decision_rank: number;
  decision_basis: string;
  decision_outcome: string;
  decision_probability: number;
  decision_market_difference?: number;
  point_in_time_verified: boolean;
  odds_observed_at?: string;
  stake_amount: number;
};

export type AutoBettingRoundRecord = {
  season_id: string;
  round: number;
  match_id: number;
  status: string;
  home_team: string;
  away_team: string;
  stake_amount: number;
  quoted_odds?: number;
  potential_return?: number;
  realized_return?: number;
  realized_profit?: number;
  skip_reason?: string;
  bet_reference?: string;
  placed_at?: string;
  settled_at?: string;
  created_at: string;
};

export type AutoBettingState = {
  settings: AutoBettingSettings;
  runtime: AutoBettingRuntime;
  current_season_id?: string;
  season_summary?: AutoBettingSeasonSummary;
  next_candidates?: AutoBettingCandidate[];
  /** @deprecated Use next_candidates. */
  next_candidate?: AutoBettingCandidate;
  recent_rounds: AutoBettingRoundRecord[];
};
