ALTER TABLE auto_betting_settings
ADD COLUMN IF NOT EXISTS stake_reset_at TIMESTAMPTZ;

UPDATE auto_betting_settings
SET stake_reset_at = COALESCE(stake_reset_at, updated_at, NOW())
WHERE stake_reset_at IS NULL;

ALTER TABLE auto_betting_settings
ALTER COLUMN stake_reset_at SET DEFAULT NOW();

ALTER TABLE auto_betting_settings
ALTER COLUMN stake_reset_at SET NOT NULL;
