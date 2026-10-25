BEGIN;

-- Phase 4 authentication is intentionally single-owner.  The configured
-- email is persisted as an account identity for foreign keys and preferences,
-- but the application configuration remains the authorization source of truth.
CREATE TABLE IF NOT EXISTS auth_authorized_accounts (
    account_id text PRIMARY KEY CHECK (account_id = 'primary'),
    normalized_email text NOT NULL UNIQUE,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_magic_link_tokens (
    token_hash char(64) PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    account_id text NOT NULL REFERENCES auth_authorized_accounts (account_id) ON DELETE RESTRICT,
    redirect_path text NOT NULL DEFAULT '/dashboard',
    requested_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    consumed_session_hash char(64) CHECK (consumed_session_hash IS NULL OR consumed_session_hash ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > requested_at),
    CHECK (consumed_at IS NULL OR consumed_at >= requested_at)
);

CREATE INDEX IF NOT EXISTS auth_magic_link_tokens_expiry_idx
    ON auth_magic_link_tokens (expires_at, consumed_at);

CREATE TABLE IF NOT EXISTS auth_sessions (
    session_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    session_hash char(64) PRIMARY KEY CHECK (session_hash ~ '^[0-9a-f]{64}$'),
    account_id text NOT NULL REFERENCES auth_authorized_accounts (account_id) ON DELETE RESTRICT,
    csrf_token_hash char(64) NOT NULL CHECK (csrf_token_hash ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    revoked_at timestamptz,
    user_agent text,
    client_ip_hash char(64),
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX IF NOT EXISTS auth_sessions_account_idx
    ON auth_sessions (account_id, revoked_at, expires_at DESC);

CREATE TABLE IF NOT EXISTS auth_login_rate_limits (
    rate_key text PRIMARY KEY,
    window_started_at timestamptz NOT NULL,
    request_count integer NOT NULL CHECK (request_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_login_rate_limits_updated_idx
    ON auth_login_rate_limits (updated_at);

CREATE TABLE IF NOT EXISTS auth_audit_events (
    auth_audit_event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type text NOT NULL CHECK (event_type IN (
        'login_requested', 'login_rejected', 'login_sent', 'login_failed',
        'login_consumed', 'session_created', 'session_revoked', 'logout',
        'settings_updated'
    )),
    account_id text REFERENCES auth_authorized_accounts (account_id) ON DELETE SET NULL,
    email_hash char(64) CHECK (email_hash IS NULL OR email_hash ~ '^[0-9a-f]{64}$'),
    session_hash char(64) CHECK (session_hash IS NULL OR session_hash ~ '^[0-9a-f]{64}$'),
    client_ip_hash char(64),
    success boolean NOT NULL,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_audit_events_created_idx
    ON auth_audit_events (created_at DESC, event_type);

CREATE TABLE IF NOT EXISTS user_preferences (
    account_id text PRIMARY KEY REFERENCES auth_authorized_accounts (account_id) ON DELETE CASCADE,
    timezone text NOT NULL DEFAULT 'UTC',
    theme text NOT NULL DEFAULT 'dark' CHECK (theme IN ('dark', 'light', 'system')),
    refresh_interval_seconds integer NOT NULL DEFAULT 60 CHECK (refresh_interval_seconds BETWEEN 30 AND 900),
    date_format text NOT NULL DEFAULT 'iso' CHECK (date_format IN ('iso', 'locale')),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version)
VALUES ('009_phase4_auth')
ON CONFLICT (version) DO NOTHING;

COMMIT;
