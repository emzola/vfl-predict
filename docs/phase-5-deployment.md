# Phase 5 production deployment runbook

Status: deployment preparation implemented locally. No Contabo VPS, DNS zone,
Resend account, or production email delivery was accessed by Phase 5.

The production topology is deliberately conservative:

```text
Cloudflare proxy -> Caddy :80/:443
                    |-> Next.js web (private Docker network)
                    |-> Go API (private Docker network)
PostgreSQL, live monitor, prediction worker, settlement worker, and migration
runner remain private. The workers do not depend on a browser session.
```

The Python service is not included in the production Compose file because the
production prediction worker currently uses the Go `bookmaker_first` policy:
it uses normalized bookmaker-implied 1X2 probabilities when a complete,
point-in-time-verified line was observed before the prediction cutoff, and
falls back to the fitted Go Poisson goal model when that line is unavailable.
LightGBM remains a research/training path and is not scheduled on the VPS.

The production Compose configuration runs the Go prediction worker in
`-auto-freeze` mode. It refreshes provisional predictions for the next
eligible round and creates a separate official frozen run inside the
configured pre-kickoff lead window. `PREDICTION_SAFETY_BUFFER_SECONDS` remains
the hard cutoff protection; `PREDICTION_FREEZE_LEAD_SECONDS` controls how far
before that cutoff the worker attempts the freeze. Once a complete frozen or
settled run exists, the worker advances to the next round.

## Files

- `infra/docker/docker-compose.production.yml` is the production topology.
- `infra/docker/Caddyfile.production` is the Caddy configuration.
- `infra/docker/.env.production.example` is a placeholder-only environment template.
- `infra/docker/Dockerfile.worker` builds the independent Go workers.
- `scripts/deploy-production.sh` performs a locked, digest-checked deployment.
- `scripts/rollback-production.sh` performs an explicitly authorized image-only rollback.
- `scripts/bootstrap-vps.sh` prepares a host without creating credentials or changing DNS.
- `.github/workflows/phase5-production.yml` validates, builds, publishes, and conditionally deploys.
- `.github/workflows/rollback-production.yml` provides a manual rollback gate.
- `infra/systemd/` provides a lightweight read-only storage/container health timer.

## Values that must be supplied out of band

No value below is invented or stored in the repository.

| Location | Required value |
| --- | --- |
| GitHub secret | `PRODUCTION_HOST` — VPS hostname or IP |
| GitHub secret | `PRODUCTION_SSH_USER` — restricted deployment user |
| GitHub secret | `PRODUCTION_SSH_PRIVATE_KEY` — dedicated key, never a personal key |
| GitHub secret | `PRODUCTION_KNOWN_HOSTS` — verified SSH host-key line(s) |
| GitHub variable | `PRODUCTION_APP_DIR` — defaults to `/opt/bet9ja-vfl` |
| GitHub variable | `PRODUCTION_DEPLOY_SERVICES` — optional affected-service list |
| VPS environment | `AUTHORIZED_EMAIL`, `RESEND_API_KEY`, `EMAIL_FROM`, database password, internal API token |
| Workflow-derived | `RELEASE_SHA` and `GHCR_IMAGE_PREFIX` are supplied automatically from the triggering commit and `github.repository` |
| VPS environment | documented Bet9ja endpoint URLs and provider configuration |
| Cloudflare | the actual Contabo public IPv4 for the `app` A record |

The GitHub workflow derives the actual repository owner/name from
`github.repository`; it does not hardcode one. It passes the resulting
`GHCR_IMAGE_PREFIX` to the host deployer, so it is not a value that needs to be
copied into `.env.production`. GHCR pull authentication must be configured on
the VPS with a least-privilege read-only credential. The deployment workflow
never prints or transmits that credential.

## First deployment procedure

The first deployment follows the same automatic path as every later deployment.
Complete these prerequisites before pushing or merging the first release to
`main`:

1. Confirm the Contabo VPS identity, public IPv4, operating system, and SSH host key.
2. Install Docker Engine and the Compose plugin using the vendor's current instructions.
3. Run `scripts/bootstrap-vps.sh` as a reviewed root operation. It creates only the deployment directories/user and an empty mode-0600 environment file.
4. Install the restricted SSH public key for the deployment user. Permit SSH only through the chosen firewall policy.
5. Authenticate the deployment user to GHCR using a read-only package token. Do not put that token in Git, GitHub workflow output, or a command argument.
6. Copy `infra/docker/.env.production.example` to `/opt/bet9ja-vfl/.env.production`, replace all `REPLACE_*` values out of band, and keep mode `0600`.
7. The workflow uploads the reviewed Compose/Caddy bundle and deployment scripts from the release commit; do not manually replace them on the host. Never use `docker compose down -v` and do not remove `bet9ja-vfl-production-postgres-data`.
8. Configure Cloudflare and Resend as described below.
9. Verify that the `production` GitHub environment has no required reviewers if
   fully automatic deployment is desired.
10. Push or merge the release to `main`.
11. Verify the Caddy certificate, magic-link delivery, dashboard access, worker
    leases, database health, and the first logical backup.

Every successful push or merge to `main` runs validation, builds and publishes
the release images, runs the guarded migration container, and updates the
production services. Changes to `db/migrations/*.sql` do not require separate
approval. A migration failure stops the release before application services are
updated.

## Cloudflare and Caddy

Create only the requested record after the actual VPS IP is known:

```text
Type: A
Name: app
Target: <actual Contabo VPS IPv4>
Proxy: Proxied
```

Do not add an AAAA record until IPv6 routing, Docker binding, and firewall rules
have been tested. Use Cloudflare SSL/TLS mode **Full (strict)**. Caddy stores
its certificate state in the persistent `caddy-data` volume and requests a
public certificate for `app.vflpredict.win` through its standard ACME support.
Ports 80 and 443 must reach Caddy for HTTP challenge/redirect behavior and
renewal checks. If DNS-01 becomes necessary, use a narrowly scoped Cloudflare
API token and a Caddy image that has been deliberately built with the selected
DNS provider plugin; the standard image in this repository does not assume one.

Only Caddy publishes host ports. PostgreSQL, the API, web, and workers use the
backend Docker network and have no host-published ports. The host firewall
should permit 80/443 according to the Cloudflare proxy policy and restrict SSH
to the operator's administrative path. Cloudflare IP ranges must be reviewed
from Cloudflare's current source before firewall rules are written; none are
hardcoded here.

The Caddy access log filters the `token` and `redirect` query values from magic
link URLs and removes cookies/authorization headers. All dashboard/API
responses are marked `no-store`. The Go API is not directly reachable from the
Internet, so client-supplied forwarding headers are not treated as an
authoritative public client identity at the application boundary.

## Resend setup

1. Create a Resend project/API key with the narrowest send permission available.
2. Add and verify the sender domain in Resend.
3. Copy the exact SPF/DKIM and any related records shown by Resend into the Cloudflare DNS zone. Do not invent values, overwrite an existing unrelated SPF policy, or create a second conflicting SPF record.
4. Set `EMAIL_FROM` to the verified sender and `RESEND_API_KEY` only in the protected VPS environment.
5. Send one controlled message to the authorized account and complete the real callback flow before treating delivery as verified.

The Go API uses the existing `MagicLinkMailer` interface with
`MAILER_MODE=resend`, `RESEND_API_KEY`, `RESEND_API_URL`, and `EMAIL_FROM`.
Tests use an isolated HTTP server and never call Resend. Production startup
fails closed when HTTPS, the authorized email, secure cookies, or the Resend
configuration is missing. Authentication tokens are stored hashed and are not
written to logs.

Email delivery/webhook events are not used as a prerequisite for login. A
future delivery-event webhook must have provider signature verification and an
idempotency key before it is enabled; no unverified webhook endpoint is exposed
by this deployment.

## Migration and release safety

`cmd/migrate` is the only production migration runner. It uses the existing
PostgreSQL advisory lock, ordered SQL files, and the new `schema_migrations`
checksum catalog. API and worker services set `RUN_MIGRATIONS=false` in
production. Development commands retain their previous default of applying
migrations when the variable is absent.

The GitHub Actions `deploy` job invokes the host deployer over SSH. Therefore
database migrations are part of the workflow deployment; no separate manual
migration command is required. The deployer:

- validates Compose interpolation;
- pulls SHA-tagged images;
- verifies the CI-produced image digests;
- starts/reuses PostgreSQL without touching its named volume;
- runs one migration container;
- updates only the requested application services;
- waits for API, web, and Caddy health checks;
- verifies the release label and records the previous release;
- serializes host deployments with `flock` in addition to GitHub Actions concurrency.

It never performs a database rollback. The rollback workflow restores retained
image/config bundles only after `CONFIRM_ROLLBACK=YES`; an operator must first
confirm that the target application is compatible with the already-applied
schema.

Worker replacements receive Compose's stop grace period so normal SIGTERM
handling can release leases. Do not deploy worker changes near an imminent
prediction cutoff unless the operator has verified that the transition is safe.
The `PRODUCTION_DEPLOY_SERVICES` variable can be narrowed for a frontend/API
release.

## Storage, resources, and backups

Phase 4.2 measured the local database at about 1.19 GiB and the PostgreSQL
volume at about 2.33 GB. The largest local consumer was Docker build cache,
not PostgreSQL. Production keeps logs bounded at 10 MiB x 3 files per service,
uses explicit memory/CPU limits, and never schedules LightGBM training.

The health timer reports filesystem, Docker, container, PostgreSQL health, and
memory status. Treat below 25% free disk as a warning and below 15% as
critical. Use the existing Phase 4.2 storage audit for relation/WAL detail.
Do not run indiscriminate Docker prune, volume prune, `VACUUM FULL`, or odds/
prediction deletion as deployment maintenance.

Contabo Auto Backup is the primary full-VPS recovery mechanism. Confirm in the
Contabo control panel that it is enabled and record its current retention
before deployment; its documented behavior is whole-VPS restore rather than
individual-file restore. Keep a separate protected copy of PostgreSQL logical
backups because a VPS-local dump does not protect against total VPS loss.

Example backup and isolated restore procedure:

```sh
DATABASE_URL='postgres://...' BACKUP_DIR=/opt/bet9ja-vfl/backups \
  ./scripts/postgres-logical-backup.sh

# Restore only into a separately created recovery database:
CONFIRM_RESTORE=YES DATABASE_URL='postgres://.../vfl_recovery' \
  BACKUP_FILE=/protected/path/vfl-<timestamp>.dump \
  ./scripts/postgres-logical-restore.sh
```

Never point the restore command at the live production database during a test.

## Verification commands

Local deterministic checks (no VPS, DNS, provider, or Resend access):

```powershell
go test -p 1 -count=1 ./...
go vet ./...
cd apps/web
npm ci
npm test
npm run typecheck
npm run openapi:validate
npm run build
cd ../..
docker compose -f infra/docker/docker-compose.yml config --quiet
```

Phase 5's verifier is `scripts/verify-phase5.ps1`. It checks the prior phase
verifiers where available, production Compose interpolation, workflow/script
presence, migration safety markers, and the absence of committed credentials.
The real production smoke test is intentionally separate from CI and must be
run only after the operator supplies infrastructure values.

## Known blockers and limitations

- No VPS hostname/IP, SSH key, or verified SSH host key was supplied, so no production connection was attempted.
- No GitHub repository owner/name was invented; GHCR names are derived by the workflow or supplied in the VPS environment.
- No authorized login email, Resend API key, sender, Cloudflare API token, or DNS verification value is present.
- Cloudflare DNS, Full (strict) origin TLS, Resend delivery, GHCR pull access, and real browser login remain production gates.
- The standard Caddy image has no assumed DNS-01 plugin; HTTP-01 is the default path, and a deliberate plugin image is required if the final Cloudflare setup needs DNS-01.
- A live provider request was not made by Phase 5; provider endpoint values remain an operator-controlled configuration and the existing ten-second minimum request spacing is preserved.
- Browser E2E is a deterministic local gate; it does not prove real Resend delivery or a genuine forward prediction round.
