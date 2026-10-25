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
  secondary?: Record<string, unknown>;
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
