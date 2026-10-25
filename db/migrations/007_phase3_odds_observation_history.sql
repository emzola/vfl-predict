BEGIN;

-- Keep each local observation of a normalized odds value. odds_snapshots is
-- the stable value/provenance identity; this table is the evidence timeline.
-- A later cached observation must not replace an earlier pre-kickoff
-- observation used by a frozen prediction.
CREATE TABLE IF NOT EXISTS odds_snapshot_observations (
    odds_observation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    odds_snapshot_id uuid NOT NULL REFERENCES odds_snapshots (odds_snapshot_id) ON DELETE RESTRICT,
    raw_payload_id uuid NOT NULL REFERENCES raw_payloads (raw_payload_id) ON DELETE RESTRICT,
    observed_at timestamptz NOT NULL,
    fetched_at timestamptz NOT NULL,
    source_timestamp_ms bigint NOT NULL CHECK (source_timestamp_ms > 0),
    source_timestamp_at timestamptz NOT NULL,
    observation_kind text NOT NULL CHECK (observation_kind IN ('historical_returned_after_settlement', 'live_observation', 'unknown')),
    point_in_time_verified boolean NOT NULL DEFAULT false,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (odds_snapshot_id, raw_payload_id, observed_at)
);

CREATE INDEX IF NOT EXISTS odds_snapshot_observations_lookup_idx
    ON odds_snapshot_observations (odds_snapshot_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS odds_snapshot_observations_cutoff_idx
    ON odds_snapshot_observations (observed_at DESC, point_in_time_verified);

-- Preserve an auditable baseline for rows imported before this migration.
-- Later provider polls are inserted by the repository at their true local
-- observation time and therefore retain a richer history.
INSERT INTO odds_snapshot_observations (
    odds_snapshot_id, raw_payload_id, observed_at, fetched_at,
    source_timestamp_ms, source_timestamp_at, observation_kind,
    point_in_time_verified, provenance
)
SELECT o.odds_snapshot_id, o.raw_payload_id, o.last_observed_at, o.fetched_at,
       o.source_timestamp_ms, o.source_timestamp_at, o.observation_kind,
       o.point_in_time_verified AND o.last_observed_at < f.scheduled_at,
       COALESCE(o.provenance, '{}'::jsonb) || '{"backfilled_from":"odds_snapshots.last_observed_at"}'::jsonb
FROM odds_snapshots o
JOIN fixtures f ON f.match_id = o.match_id
ON CONFLICT (odds_snapshot_id, raw_payload_id, observed_at) DO NOTHING;

INSERT INTO schema_migrations (version)
VALUES ('007_phase3_odds_observation_history')
ON CONFLICT (version) DO NOTHING;

COMMIT;
