BEGIN;

-- The active strategy is Odd/even with flat staking. The service chooses the
-- cycle outcome from the rows written since the last auto-betting toggle.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN market_key SET DEFAULT 'odd_even';

-- Start the restored cycle at its first outcome. Subsequent season changes do
-- not reset it; the service refreshes this cutoff only when auto betting is
-- turned off or re-enabled.
UPDATE auto_betting_settings
SET recovery_reset_at = now()
WHERE setting_key = 'primary';

COMMENT ON COLUMN auto_betting_settings.recovery_reset_at IS
    'Odd/even cycle cutoff set when the strategy is initialized or auto betting is toggled; season changes do not reset the cycle';

COMMENT ON COLUMN auto_betting_settings.base_stake IS
    'Configured flat stake amount used for every new auto-betting wager';

COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision, observed 1X2 probability, Odd/even cycle, and flat-stake audit data';

INSERT INTO schema_migrations (version)
VALUES ('023_phase7_odd_even_flat_staking')
ON CONFLICT (version) DO NOTHING;

COMMIT;
