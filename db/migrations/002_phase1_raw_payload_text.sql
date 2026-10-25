BEGIN;

ALTER TABLE raw_payloads
    ADD COLUMN IF NOT EXISTS payload_text text;

ALTER TABLE raw_payloads
    ALTER COLUMN payload DROP NOT NULL;

UPDATE raw_payloads
SET payload_text = payload::text
WHERE payload_text IS NULL;

ALTER TABLE raw_payloads
    ALTER COLUMN payload_text SET NOT NULL;

ALTER TABLE raw_payloads
    DROP CONSTRAINT IF EXISTS raw_payloads_payload_check;

ALTER TABLE raw_payloads
    ADD CONSTRAINT raw_payloads_payload_check
    CHECK (payload IS NOT NULL OR payload_text IS NOT NULL);

INSERT INTO schema_migrations (version)
VALUES ('002_phase1_raw_payload_text')
ON CONFLICT (version) DO NOTHING;

COMMIT;
