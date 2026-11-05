BEGIN;

-- Double Chance 12 is the active market. Existing Odd/even and Over 1.5
-- ledger rows remain historical and are not rewritten.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN market_key SET DEFAULT 'double_chance_12';

-- Start this restored strategy with a clean recovery balance. Subsequent
-- season changes do not reset recovery; the service refreshes this cutoff
-- only when auto betting is turned off or re-enabled.
UPDATE auto_betting_settings
SET recovery_reset_at = now()
WHERE setting_key = 'primary';

CREATE UNIQUE INDEX IF NOT EXISTS auto_betting_rounds_double_chance_12_one_per_round_idx
    ON auto_betting_rounds (season_id, round_no)
    WHERE market_key = 'double_chance_12';

COMMENT ON COLUMN auto_betting_settings.base_stake IS
    'Base unit used to set the intended profit for Double Chance 12 martingale recovery wagers';

COMMENT ON COLUMN auto_betting_settings.recovery_reset_at IS
    'Recovery cutoff set when auto betting is turned off; season changes do not reset recovery';

COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision and martingale recovery audit data, including the Double Chance 12 selection';

INSERT INTO schema_migrations (version)
VALUES ('025_phase7_double_chance_12_martingale')
ON CONFLICT (version) DO NOTHING;

COMMIT;
