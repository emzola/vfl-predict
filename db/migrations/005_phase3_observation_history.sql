BEGIN;

-- A cached 304 response can reuse the same raw payload while still being a
-- distinct local observation. Preserve those timestamps for freshness and
-- cutoff audits instead of collapsing them by payload identity alone.
ALTER TABLE live_feed_observations
    DROP CONSTRAINT IF EXISTS live_feed_observations_raw_payload_id_endpoint_family_request_key_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'live_feed_observations'::regclass
          AND conname = 'live_feed_observations_payload_observed_unique'
    ) THEN
        ALTER TABLE live_feed_observations
            ADD CONSTRAINT live_feed_observations_payload_observed_unique
            UNIQUE (raw_payload_id, endpoint_family, request_key, observed_at);
    END IF;
END;
$$;

INSERT INTO schema_migrations (version)
VALUES ('005_phase3_observation_history')
ON CONFLICT (version) DO NOTHING;

COMMIT;
