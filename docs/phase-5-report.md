# Phase 5 implementation and deployment report

Date: 2026-10-25

Status: deployment preparation complete; production deployment was not attempted.

## Outcome

Phase 5 deployment infrastructure is implemented locally. The repository now has a
SHA-based GitHub Actions release path, private production Compose topology, guarded
SSH deployment and image-only rollback scripts, Caddy configuration, Resend
configuration, migration integrity checks, browser smoke tests, and an operator
runbook. No VPS, DNS zone, GHCR package, or Resend account was modified.

The implementation preserves the existing prediction workers, provider request
budget, worker leases, cutoff enforcement, frozen prediction immutability,
settlement history, odds provenance, and PostgreSQL volume. The production
prediction policy is `bookmaker_first`: a complete verified pre-cutoff 1X2 line
is used when available, with a Poisson fallback recorded per fixture. LightGBM
training is not scheduled in production.

## Implemented functionality

- `.github/workflows/phase5-production.yml` validates, builds, publishes SHA-tagged
  API/worker/web images, and deploys automatically from `main` after automated
  validation and image-build checks. Third-party action references are pinned to immutable
  revisions, deployments are serialized, and SSH uses a supplied known-hosts
  value.
- `.github/workflows/rollback-production.yml` provides an explicitly confirmed,
  image-only rollback. It does not reverse database migrations.
- `infra/docker/docker-compose.production.yml` defines PostgreSQL, the migration
  runner, API, Next.js, live monitor, prediction worker, settlement worker, and
  Caddy on private networks. Only ports 80 and 443 are published.
- `infra/docker/Caddyfile.production` provides HTTPS, redirect handling, security
  headers, bounded proxying, structured access logs, and redaction of cookies,
  authorization data, and token-bearing query parameters.
- `cmd/migrate` and `services/ingest/repository.go` provide one ordered migration
  runner with PostgreSQL advisory locking, checksums, legacy-schema compatibility,
  and mismatch detection. Migration `011_phase5_migration_metadata.sql` adds the
  metadata contract.
- `services/api/resend.go` and the production configuration validation provide the
  Resend adapter without logging tokens or sending mail during CI. The existing
  authentication implementation remains the sole magic-link/session system.
- `scripts/bootstrap-vps.sh`, `scripts/deploy-production.sh`,
  `scripts/rollback-production.sh`, and `scripts/production-health-report.sh`
  provide guarded host setup, lock-protected deployment, release tracking,
  health checks, filesystem thresholds, and recovery instructions.
- Playwright tests cover unauthenticated redirect, generic magic-link request
  behavior, an eight-fixture dashboard round, and responsive navigation using
  deterministic fixtures.
- `docs/phase-5-deployment.md` contains the VPS, Cloudflare, Resend, migration,
  backup, storage, worker-transition, and first-deployment runbook.

## Verification results

The following checks passed locally:

| Check | Result |
|---|---|
| `go test -p 1 -count=1 ./...` | Passed |
| `go vet ./...` | Passed |
| `npm ci` with the managed CA when required | Passed |
| `npm test` | Passed |
| `npm run typecheck` | Passed |
| `npm run openapi:validate` | Passed |
| `npm run build` | Passed |
| `npm audit --audit-level=high` | Passed; no high-severity findings |
| `npm run test:e2e` | Passed; 4 deterministic tests |
| Development and production Compose config validation | Passed |
| API, worker, and web production image builds | Passed |
| Caddy configuration validation | Passed |
| Bash deployment script syntax checks | Passed |
| GitHub Actions YAML parsing | Passed |
| Migration apply and second-run idempotency against disposable PostgreSQL | Passed |
| `scripts/verify-phase5.ps1` | Passed |

The npm audit result still reports three moderate development-tool advisories in
the Tailwind/PostCSS dependency chain. They are not high-severity production
findings; upgrading them would require a separately reviewed Tailwind major
version change.

The disposable migration test applied all 11 migrations, recorded checksums, and
completed a second run without changing the schema. The test database and local
development containers were not used as production infrastructure.

## Production verification status

Production verification is intentionally incomplete:

- No VPS address, SSH identity, repository owner/name, authorized email, Resend
  API key, sender address, or GHCR pull credential was supplied, so none was
  invented or committed.
- No SSH connection, Docker pull, production migration, container startup,
  authentication delivery, worker run, or rollback was performed.
- No Cloudflare DNS or SSL/TLS setting was changed. The required record remains
  an operator action: proxied `A app -> <Contabo VPS IPv4>` with Cloudflare
  **Full (strict)**.
- No real Resend email was sent. Delivery and login completion remain a required
  first-deployment gate.
- No live Bet9ja request was made by Phase 5 CI or verification.
- Contabo Auto Backup status/retention remains an operator verification item;
  PostgreSQL logical backup/restore procedures are documented but were not run
  against production.

## Required protected configuration

GitHub production environment secrets:

- `PRODUCTION_HOST`
- `PRODUCTION_SSH_USER`
- `PRODUCTION_SSH_PRIVATE_KEY`
- `PRODUCTION_KNOWN_HOSTS`

GitHub variables:

- Optional `PRODUCTION_APP_DIR` and `PRODUCTION_DEPLOY_SERVICES`.

The VPS-only `.env.production` must supply the actual GHCR image prefix, database
password, internal API token, authorized email, Resend API key, verified sender,
certificate contact, documented provider URLs, and release SHA. These values are
represented only by placeholders in `infra/docker/.env.production.example`.

## Security and operational boundaries

- Secrets are excluded from the repository; the public-repository workflow does
  not expose production credentials to pull-request code.
- Deployments require `main`, successful validation, immutable image tags, an
  SSH known-hosts file, GitHub concurrency, and a host-side lock.
- The deployment never runs `docker compose down -v`, removes the PostgreSQL
  volume, or automatically reverses migrations.
- The migration runner stops the release before incompatible application services
  are started when migration validation fails.
- Caddy and Compose do not publish PostgreSQL, worker, or diagnostic ports.
- Disk monitoring warns below 25% free space and treats below 15% as critical;
  no indiscriminate Docker prune is configured.
- The production worker set uses the existing conservative provider request
  spacing and is independent of dashboard sessions.

## Next operator-gated steps

1. Complete the prerequisites in `docs/phase-5-deployment.md`: VPS hardening,
   restricted deployment user, firewall, GHCR read-only pull access, protected
   `.env.production`, Cloudflare record, and Resend domain verification.
2. Run the bootstrap, then push or merge the first release to `main`.
3. Verify Caddy certificate issuance, the real Resend magic-link flow, protected
   dashboard access, worker leases, prediction cutoffs, settlement, and a logical
   backup/isolated restore.
4. Record those results. Subsequent successful pushes or merges to `main` follow
   the same automated deployment path.
5. Review incompatible or destructive schema changes using the normal release
   process, and defer worker changes near an imminent prediction cutoff.

Until those steps are completed, the platform is deployment-ready locally but not
production-verified.
