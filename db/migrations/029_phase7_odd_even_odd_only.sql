BEGIN;

-- New Odd/even bets always use the Odd outcome. Existing ledger rows remain
-- historical, and the recovery cutoff is intentionally unchanged.
COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision ranking, fixed Odd outcome, and martingale recovery audit data';

INSERT INTO schema_migrations (version)
VALUES ('029_phase7_odd_even_odd_only')
ON CONFLICT (version) DO NOTHING;

COMMIT;
