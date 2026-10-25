# Internal operational API

cmd/ops-api serves read-only /internal/v1/* operational endpoints. It binds
to 127.0.0.1:8090 by default and refuses non-loopback binds unless an
INTERNAL_API_TOKEN/-internal-token is configured.

Phase 4 also exposes authenticated /api/v1/* application endpoints for the
private dashboard. Set AUTHORIZED_EMAIL and APP_BASE_URL to enable magic-link
issuance. Tokens and session identifiers are stored only as hashes in
PostgreSQL; the default development mailer stores a link in memory and does
not log it. Production delivery uses the configured webhook mailer abstraction.

Dashboard endpoints read persisted season, fixture, odds, prediction, model,
settlement, lease, and job records. They do not call Bet9ja and do not start
workers. Sensitive endpoints require the server-side session cookie and
state-changing endpoints also require the CSRF double-submit token.
