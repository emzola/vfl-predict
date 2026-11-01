BEGIN;

-- Recovery history spans seasons. This nullable cutoff is written only when
-- auto betting is turned off, so enabling it again continues the prior cycle.
ALTER TABLE auto_betting_settings
    ADD COLUMN IF NOT EXISTS recovery_reset_at TIMESTAMPTZ;

COMMENT ON COLUMN auto_betting_settings.recovery_reset_at IS
    'Recovery-cycle cutoff set when auto betting is turned off; season changes do not reset the cycle';

INSERT INTO schema_migrations (version)
VALUES ('021_phase7_cross_season_recovery')
ON CONFLICT (version) DO NOTHING;

COMMIT;
