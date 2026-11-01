BEGIN;

-- Flat staking is now the active application strategy: every new placed bet
-- uses the configured base_stake, regardless of settled results, open bets, or
-- the live quote. Existing ledger rows remain unchanged for auditability.
COMMENT ON COLUMN auto_betting_settings.base_stake IS
    'Configured flat stake amount used for every new auto-betting wager';

COMMENT ON COLUMN auto_betting_rounds.stake_amount IS
    'Stake amount recorded for the wager; new strategy rows use base_stake';

-- Keep the quote threshold as a validity/audit floor, not a stake-sizing rule.
UPDATE auto_betting_settings
SET minimum_odds = 1.00
WHERE setting_key = 'primary';

ALTER TABLE auto_betting_settings
    ALTER COLUMN minimum_odds SET DEFAULT 1.00;

ALTER TABLE auto_betting_rounds
    ALTER COLUMN minimum_odds SET DEFAULT 1.00;

INSERT INTO schema_migrations (version)
VALUES ('019_phase7_flat_staking')
ON CONFLICT (version) DO NOTHING;

COMMIT;
