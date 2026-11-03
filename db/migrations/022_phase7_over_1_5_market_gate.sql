BEGIN;

-- The active strategy selects one match per round from the normalized observed
-- 1X2 market, then bets that match on Over 1.5 goals.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN market_key SET DEFAULT 'over_1_5';

-- Older Over 1.5 rows may contain multiple fixtures per round. Scope the new
-- uniqueness guard to rows written by this strategy so that audit history is
-- not rewritten or made impossible to migrate.
CREATE UNIQUE INDEX IF NOT EXISTS auto_betting_rounds_over_1_5_one_per_round_idx
    ON auto_betting_rounds (season_id, round_no)
    WHERE market_key = 'over_1_5'
      AND decision_payload->>'selection_rule' = 'highest_observed_1x2_market_probability_above_60';

COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision and martingale audit data, including the normalized observed 1X2 probability and Over 1.5 selection';

INSERT INTO schema_migrations (version)
VALUES ('022_phase7_over_1_5_market_gate')
ON CONFLICT (version) DO NOTHING;

COMMIT;
