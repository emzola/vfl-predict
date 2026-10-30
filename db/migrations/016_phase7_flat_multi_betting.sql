BEGIN;

-- Auto betting decisions are recorded per fixture. This allows one frozen
-- round to produce multiple independent Over 1.5 bets.
ALTER TABLE auto_betting_rounds
    DROP CONSTRAINT IF EXISTS auto_betting_rounds_season_id_round_no_key;

ALTER TABLE auto_betting_rounds
    DROP CONSTRAINT IF EXISTS auto_betting_rounds_decision_rank_check;

ALTER TABLE auto_betting_rounds
    ADD CONSTRAINT auto_betting_rounds_season_round_match_key
    UNIQUE (season_id, round_no, match_id);

-- Kept for historical ledger compatibility; the application no longer uses
-- this column as an eligibility gate. New rows record 1.00 as the legacy
-- value because every valid Over 1.5 quote is now accepted.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN minimum_odds SET DEFAULT 1.00;

ALTER TABLE auto_betting_settings
    ALTER COLUMN minimum_odds SET DEFAULT 1.00;

INSERT INTO schema_migrations (version)
VALUES ('016_phase7_flat_multi_betting')
ON CONFLICT (version) DO NOTHING;

COMMIT;
