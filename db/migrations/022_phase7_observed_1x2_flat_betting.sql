BEGIN;

-- The active strategy now selects home or away from the normalized observed
-- 1X2 market when that side's probability is strictly above 50%.
ALTER TABLE auto_betting_rounds
    ALTER COLUMN market_key SET DEFAULT '1x2';

-- The old index only applied to the retired Odd/even strategy. The existing
-- season/round/match uniqueness from migration 016 permits multiple matches
-- in one round while preventing duplicate decisions for a match.
DROP INDEX IF EXISTS auto_betting_rounds_odd_even_one_per_round_idx;

COMMENT ON COLUMN auto_betting_settings.base_stake IS
    'Configured flat stake amount used for every qualifying 1X2 wager';

COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision audit data, including the observed normalized 1X2 probability and selected home or away outcome';

INSERT INTO schema_migrations (version)
VALUES ('022_phase7_observed_1x2_flat_betting')
ON CONFLICT (version) DO NOTHING;

COMMIT;
