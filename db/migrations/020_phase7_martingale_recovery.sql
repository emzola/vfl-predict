BEGIN;

-- Martingale recovery is now the active application strategy. The service
-- records the recovery balances in decision_payload while preserving the
-- existing stake and result columns for ledger compatibility.
COMMENT ON COLUMN auto_betting_settings.base_stake IS
    'Base unit used to set the intended profit for martingale recovery wagers';

COMMENT ON COLUMN auto_betting_rounds.decision_payload IS
    'Decision and recovery audit data, including losses, missed profits, and the current intended profit';

INSERT INTO schema_migrations (version)
VALUES ('020_phase7_martingale_recovery')
ON CONFLICT (version) DO NOTHING;

COMMIT;
