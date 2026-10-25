# PostgreSQL development environment

docker-compose.yml starts a local PostgreSQL 16 instance on
127.0.0.1:5432 by default, plus private-network API and Next.js containers.
The web container is the only local host entry point (127.0.0.1:3000 by
default); PostgreSQL and the Go API are not published to the host.

Set POSTGRES_PASSWORD in an untracked .env file or the current shell before starting the service. The repository contains only placeholders.
Set INTERNAL_API_TOKEN in the same untracked environment because the API
container binds to its private Docker network rather than loopback.
Set AUTHORIZED_EMAIL, APP_BASE_URL, and a production mailer before using
magic-link authentication. The default development mailer keeps links in
memory and does not log them.
The PostgreSQL host port is configurable with `POSTGRES_HOST_PORT` and
defaults to `5432`. Use an alternate local port when another service owns the
default, and update DATABASE_URL accordingly. Keep the password in an
untracked .env file or process environment.

Workers remain independent commands and are not started by the frontend or
dashboard. Run the live monitor, prediction worker, and settlement process
separately with their existing operator safeguards.

For the Phase 4.1 disposable smoke test, use an explicit Compose project and
local-only placeholder values. The verification script also applies migrations,
checks the web-to-API route, verifies private port exposure, and exercises API
and web restart recovery:

```powershell
./scripts/verify-phase41.ps1 `
  -DatabaseUrl 'postgres://vfl:local-only@127.0.0.1:55433/vfl_phase41?sslmode=disable' `
  -NpmCaFile 'C:\secure\managed-root.pem' `
  -RunComposeSmoke `
  -ComposeProject 'bet9ja-vfl-phase41-smoke' `
  -ComposeWebPort 3301 `
  -ComposePostgresPort 55434
```

The command is intended for an isolated disposable database. It does not use
provider credentials, send email, or deploy to a server.

`Caddyfile.example` is a reference TLS reverse-proxy configuration. Replace
the example hostname, terminate HTTPS at the reverse proxy, and forward only
to the private `web` service. The example is not a production deployment and
must be combined with firewall rules, secret management, logging, and database
backup/restore procedures.

## Phase 5 production bundle

`docker-compose.production.yml` is separate from the local development
Compose file. It publishes only Caddy on ports 80 and 443, keeps PostgreSQL,
the Go API, web server, and workers on private Docker networks, and uses the
persistent `bet9ja-vfl-production-postgres-data` volume. It does not include
the Python research/training service because production inference is currently
provided by the Go statistical engine.

Copy `.env.production.example` to the VPS only as a template; it contains no
usable credentials. The real `/opt/bet9ja-vfl/.env.production` must be created
out of band with mode 600. Use `scripts/deploy-production.sh` for deployments
and `scripts/rollback-production.sh` for an explicitly authorized image-only
rollback. Neither script runs `docker compose down -v` or reverses database
migrations.

`RELEASE_SHA` and `GHCR_IMAGE_PREFIX` are intentionally not stored in
`.env.production`. The GitHub Actions deployment passes the exact commit SHA
and derives the GHCR prefix from `github.repository` before invoking
`scripts/deploy-production.sh`. This prevents the VPS environment file from
selecting a stale or manually guessed image release or repository path.

The production migration service runs `cmd/migrate` once under the PostgreSQL
advisory lock. API and worker containers set `RUN_MIGRATIONS=false`; local
commands preserve their existing migration-on-start default. See
`docs/phase-5-deployment.md` for the first-deployment, Cloudflare, Resend,
GHCR, backup, firewall, and rollback procedures.

## Phase 4.2 storage policy

Migration `010_phase42_storage_policy.sql` adds a retention-policy catalog and
storage-audit snapshots. Applying it does not remove data. The default policy
keeps fixtures/results, verified odds, frozen predictions, settlements, and
research artifacts permanently. Raw provider bodies and unverified live odds
are marked archiveable only after a lossless provenance-preserving archive is
approved. Operational cleanup is exposed as a dry-run candidate report only.

Do not run `docker system prune`, `docker volume prune`, `VACUUM FULL`, or a
database cleanup job as part of routine deployment. Review the Phase 4.2 audit,
take a logical backup, and obtain explicit approval before any destructive
action. PostgreSQL autovacuum settings for append-heavy evidence tables are
set by migration 010; routine maintenance should use ordinary `VACUUM
(ANALYZE)` only when the measured bloat and maintenance windows justify it.

For the planned 8 GB VPS, keep PostgreSQL, the API, the web process, and live
workers within explicit memory budgets. Run LightGBM training as a bounded,
one-thread batch job outside the live worker budget (or on a separate host),
and never start training automatically from the dashboard. Keep PostgreSQL
and Docker data on the persistent volume and alert below 25% free disk, with
an emergency threshold below 15%.
