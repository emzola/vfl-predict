BEGIN;

-- The active auto-betting market is now Odd/even, always selecting even.
-- Existing ledger rows retain their original market key for auditability.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN market_key SET DEFAULT 'odd_even';

-- The only quote gate is provider validity (> 1.00); keep the column as an
-- audit field for both legacy and new ledger rows.
UPDATE auto_betting_settings
SET minimum_odds = 1.00
WHERE setting_key = 'primary';

ALTER TABLE auto_betting_settings
    ALTER COLUMN minimum_odds SET DEFAULT 1.00;

ALTER TABLE auto_betting_rounds
    ALTER COLUMN minimum_odds SET DEFAULT 1.00;

-- Legacy Over 1.5 rows may contain more than one fixture per round because
-- migration 016 temporarily allowed that shape. Enforce one active Odd/even
-- decision per round for the new strategy without rewriting that history.
CREATE UNIQUE INDEX IF NOT EXISTS auto_betting_rounds_odd_even_one_per_round_idx
    ON auto_betting_rounds (season_id, round_no)
    WHERE market_key = 'odd_even';

INSERT INTO schema_migrations (version)
VALUES ('018_phase7_odd_even_betting')
ON CONFLICT (version) DO NOTHING;

COMMIT;
