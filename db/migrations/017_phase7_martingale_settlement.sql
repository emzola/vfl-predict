BEGIN;

-- Restore the minimum required quote for the active martingale strategy.
-- Existing ledger rows retain the minimum that applied when they were placed.
UPDATE auto_betting_settings
SET minimum_odds = 1.20
WHERE setting_key = 'primary'
  AND minimum_odds < 1.20;

ALTER TABLE auto_betting_settings
    ALTER COLUMN minimum_odds SET DEFAULT 1.20;

ALTER TABLE auto_betting_rounds
    ALTER COLUMN minimum_odds SET DEFAULT 1.20;

INSERT INTO schema_migrations (version)
VALUES ('017_phase7_martingale_settlement')
ON CONFLICT (version) DO NOTHING;

COMMIT;
