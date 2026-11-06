BEGIN;

-- New Odd/even bets use an eight-bet cycle. Existing ledger rows remain
-- historical, and the recovery cutoff is intentionally unchanged.
COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision ranking, eight-step Odd/even cycle, and martingale recovery audit data';

INSERT INTO schema_migrations (version)
VALUES ('027_phase7_odd_even_eight_step_cycle')
ON CONFLICT (version) DO NOTHING;

COMMIT;
