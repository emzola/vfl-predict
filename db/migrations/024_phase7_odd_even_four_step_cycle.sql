BEGIN;

-- Start the new four-step Odd/even cycle at its first outcome. Later season
-- changes do not reset the cycle; the service refreshes this cutoff only when
-- auto betting is turned off or re-enabled.
UPDATE auto_betting_settings
SET recovery_reset_at = now()
WHERE setting_key = 'primary';

INSERT INTO schema_migrations (version)
VALUES ('024_phase7_odd_even_four_step_cycle')
ON CONFLICT (version) DO NOTHING;

COMMIT;
