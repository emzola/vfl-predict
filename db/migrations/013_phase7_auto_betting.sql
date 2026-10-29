BEGIN;

CREATE TABLE IF NOT EXISTS auto_betting_settings (
    setting_key text PRIMARY KEY CHECK (setting_key = 'primary'),
    enabled boolean NOT NULL DEFAULT false,
    base_stake numeric(18, 2) NOT NULL DEFAULT 1000 CHECK (base_stake > 0),
    profit_target numeric(18, 2) NOT NULL DEFAULT 3000 CHECK (profit_target > 0),
    minimum_odds numeric(8, 2) NOT NULL DEFAULT 1.20 CHECK (minimum_odds >= 1.00),
    updated_by text REFERENCES auth_authorized_accounts (account_id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO auto_betting_settings (setting_key)
VALUES ('primary')
ON CONFLICT (setting_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS auto_betting_runtime (
    runtime_key text PRIMARY KEY CHECK (runtime_key = 'primary'),
    state text NOT NULL DEFAULT 'disabled' CHECK (state IN (
        'disabled',
        'idle',
        'ready',
        'placing',
        'awaiting_settlement',
        'paused_profit_target',
        'login_required',
        'error'
    )),
    worker_id text,
    current_season_id text REFERENCES seasons (season_id) ON DELETE SET NULL,
    current_round_no smallint CHECK (current_round_no IS NULL OR current_round_no BETWEEN 1 AND 30),
    current_match_id bigint REFERENCES fixtures (match_id) ON DELETE SET NULL,
    current_url text,
    last_heartbeat_at timestamptz,
    last_login_at timestamptz,
    last_error text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO auto_betting_runtime (runtime_key)
VALUES ('primary')
ON CONFLICT (runtime_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS auto_betting_rounds (
    auto_betting_round_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    season_id text NOT NULL REFERENCES seasons (season_id) ON DELETE RESTRICT,
    round_no smallint NOT NULL CHECK (round_no BETWEEN 1 AND 30),
    prediction_run_id uuid REFERENCES prediction_runs (prediction_run_id) ON DELETE RESTRICT,
    prediction_revision_id uuid REFERENCES prediction_revisions (prediction_revision_id) ON DELETE RESTRICT,
    match_id bigint REFERENCES fixtures (match_id) ON DELETE RESTRICT,
    home_team text NOT NULL DEFAULT '',
    away_team text NOT NULL DEFAULT '',
    decision_rank integer NOT NULL DEFAULT 1 CHECK (decision_rank = 1),
    market_key text NOT NULL DEFAULT 'over_1_5',
    status text NOT NULL CHECK (status IN ('skipped', 'placed', 'won', 'lost', 'void', 'error')),
    stake_amount numeric(18, 2) NOT NULL DEFAULT 0 CHECK (stake_amount >= 0),
    minimum_odds numeric(8, 2) NOT NULL DEFAULT 1.20 CHECK (minimum_odds >= 1.00),
    quoted_odds numeric(8, 2),
    potential_return numeric(18, 2),
    realized_return numeric(18, 2),
    realized_profit numeric(18, 2),
    skip_reason text,
    bet_reference text,
    result_home_score integer CHECK (result_home_score IS NULL OR result_home_score >= 0),
    result_away_score integer CHECK (result_away_score IS NULL OR result_away_score >= 0),
    decision_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    placed_at timestamptz,
    settled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (season_id, round_no)
);

CREATE INDEX IF NOT EXISTS auto_betting_rounds_status_idx
    ON auto_betting_rounds (status, season_id, round_no DESC);

CREATE INDEX IF NOT EXISTS auto_betting_rounds_match_idx
    ON auto_betting_rounds (match_id, status);

INSERT INTO schema_migrations (version)
VALUES ('013_phase7_auto_betting')
ON CONFLICT (version) DO NOTHING;

COMMIT;
