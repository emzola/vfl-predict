BEGIN;

-- Odd/even is the active market again. The six-bet cycle starts with odd and
-- is counted from settled or placed Odd/even ledger rows after the existing
-- recovery cutoff. The cutoff is intentionally unchanged so the active
-- martingale balance carries over this market change and across seasons.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN market_key SET DEFAULT 'odd_even';

CREATE UNIQUE INDEX IF NOT EXISTS auto_betting_rounds_odd_even_one_per_round_idx
    ON auto_betting_rounds (season_id, round_no)
    WHERE market_key = 'odd_even';

COMMENT ON COLUMN auto_betting_settings.recovery_reset_at IS
    'Recovery cutoff set when auto betting is turned off; market changes and season changes do not reset recovery';

COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision ranking, Odd/even cycle, and martingale recovery audit data';

INSERT INTO schema_migrations (version)
VALUES ('026_phase7_odd_even_six_step_cycle')
ON CONFLICT (version) DO NOTHING;

COMMIT;
