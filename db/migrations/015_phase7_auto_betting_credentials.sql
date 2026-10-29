ALTER TABLE auto_betting_settings
ADD COLUMN IF NOT EXISTS bet9ja_username_ciphertext text,
ADD COLUMN IF NOT EXISTS bet9ja_password_ciphertext text,
ADD COLUMN IF NOT EXISTS bet9ja_username_masked text NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS credentials_updated_by text REFERENCES auth_authorized_accounts (account_id) ON DELETE SET NULL,
ADD COLUMN IF NOT EXISTS credentials_updated_at timestamptz;
