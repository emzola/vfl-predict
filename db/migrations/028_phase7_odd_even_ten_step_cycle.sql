BEGIN;

-- New Odd/even bets use a ten-bet cycle. Existing ledger rows remain
-- historical, and the recovery cutoff is intentionally unchanged.
COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision ranking, ten-step Odd/even cycle, and martingale recovery audit data';

INSERT INTO schema_migrations (version)
VALUES ('028_phase7_odd_even_ten_step_cycle')
ON CONFLICT (version) DO NOTHING;

COMMIT;
