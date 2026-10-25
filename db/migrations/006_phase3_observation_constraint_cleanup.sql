BEGIN;

-- PostgreSQL truncates the automatically generated name of the original
-- three-column unique constraint at 63 bytes. Remove that exact legacy name so
-- migration 005's observed_at-aware constraint is the only conflict target.
ALTER TABLE live_feed_observations
    DROP CONSTRAINT IF EXISTS live_feed_observations_raw_payload_id_endpoint_family_reque_key;

INSERT INTO schema_migrations (version)
VALUES ('006_phase3_observation_constraint_cleanup')
ON CONFLICT (version) DO NOTHING;

COMMIT;
