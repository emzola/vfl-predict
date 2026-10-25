# Phase 4 implementation and verification report

Date: 2026-10-25  
Scope: private dashboard, single-user passwordless authentication, authenticated
application API, and Docker Compose preparation

> Verification note: the frontend/npm and PostgreSQL integration omissions in
> this earlier Phase 4 report were resolved by [Phase 4.1](phase-4.1-report.md).
> This report remains the implementation record for Phase 4; use the Phase
> 4.1 report for current verification status.

## Executive result

The Phase 4 application boundary is implemented without changing the provider
contract, prediction algorithms, worker schedules, or historical odds
provenance. The Next.js frontend is a private App Router dashboard; the Go API
owns authorization and database access; workers remain independent of browser
sessions. No public registration, password authentication, automated betting,
payment, or production deployment was added.

The repository-native Go and offline verification suites pass. The frontend
package could not be installed or built in this execution environment because
the npm registry certificate could not be validated, so no unverified
`package-lock.json` was generated and the Docker web image remains explicitly
gated on obtaining a verified lockfile.

## Delivered

### Authenticated application API

- Added `db/migrations/009_phase4_auth.sql` for the configured primary
  account, single-use magic-link token hashes, server-side session hashes,
  CSRF token hashes, login rate-limit windows, authentication audit events,
  and user display preferences.
- Added passwordless login and verification routes under
  `/api/v1/auth/*`. Only `AUTHORIZED_EMAIL` is eligible; responses for
  unauthorized addresses are generic.
- Tokens and session secrets are generated with `crypto/rand` and stored only
  as SHA-256 hashes. Link consumption is atomic and expiry/replay safe.
- Sessions use opaque HttpOnly cookies, optional Secure cookies, SameSite Lax,
  server-side expiry/revocation, and per-request lookup. State-changing
  requests require same-origin checks and a CSRF double-submit token.
- Redirects are restricted to `/dashboard` paths and links use configured
  `APP_BASE_URL`; untrusted Host headers are not used to construct links.
- The default memory mailer is development/test-only. Production delivery is
  an explicit webhook adapter configured through environment variables and does
  not log tokens or provider secrets.

### Dashboard API and frontend

- Added typed authenticated routes for overview, seasons/rounds, predictions,
  match detail/odds, performance, paper testing, operations, settings, and
  session management.
- Prediction history preserves immutable revisions and supports bounded season,
  round, status, and model filters. Future projections are grouped by season
  and round, and prediction responses expose stored 1X2 odds, normalized
  bookmaker probabilities, model-versus-market differences, secondary model
  payloads, and latest settlement status when those records exist.
- Dashboard reads are bounded database queries. They do not call Bet9ja or
  start workers, and response payloads retain freshness and odds provenance.
- Added a responsive dark-first Next.js frontend with overview, predictions,
  future rounds, match analysis, history, performance, paper testing,
  operations, settings, and login views.
- The frontend uses same-origin API requests and a conservative browser refresh
  only for mutable overview data. It never connects directly to PostgreSQL.
- Added frontend container preparation with standalone Next output and a
  private Docker network. PostgreSQL and the Go API are not host-published by
  Compose; the web entry point binds to loopback by default.

### Contracts and documentation

- Extended `packages/contracts/openapi.yaml` with the Phase 4 application
  routes, cookie/CSRF security schemes, and provenance wording.
- Updated the API assumptions register with the Phase 4 provenance, mailer,
  frontend-build, and missing-data limitations. Existing provider assumptions
  remain unresolved unless explicitly verified in prior phase reports.
- Updated Compose, environment examples, migration documentation, API/web
  READMEs, and the Phase 4 verifier. The supplied specification and seven
  provider sample/endpoint response files remain ignored by Git; no credentials
  were added and no commit was created.

## Verification

The following native-runtime checks completed successfully:

```powershell
go test -mod=readonly ./...
go vet ./...
./scripts/verify-phase4.ps1
```

The Phase 4 verifier reran the available Phase 3.6, Phase 3.5, Phase 3, Phase
2, and Go API checks, confirmed zero provider requests, checked the Compose
configuration, and passed the authentication unit tests. The existing Phase
3.5 Python/LightGBM checks and Phase 3.6 offline checks also passed through
the chained verifier.

The following checks were intentionally not claimed as passed:

- PostgreSQL-backed Phase 4 integration was skipped because
  `PHASE4_DATABASE_URL` was not configured. The integration test is
  non-destructive and verifies the six Phase 4 tables after applying all
  migrations. A disposable dedicated database is still required for positive
  token-consumption, replay, session-revocation, and concurrent-consumption
  tests.
- Frontend Vitest, TypeScript, and Next build were skipped because
  `apps/web/package-lock.json` and installed dependencies are unavailable. npm
  returned `UNABLE_TO_VERIFY_LEAF_SIGNATURE` while contacting the registry.
  The Dockerfile deliberately retains reproducible `npm ci` rather than
  weakening TLS or silently using an unlocked install.
- A YAML parser was not available in the environment for an independent
  OpenAPI parse. Compose parsing passed; the OpenAPI document remains subject
  to the normal contract-tool validation once its toolchain is available.

## Data and operational limitations

- The dashboard cannot show genuine forward performance until provider-backed
  frozen predictions are settled with eligible pre-cutoff odds. It displays an
  explicit empty/pending state rather than zero-valued claims.
- Historical odds returned after settlement remain retrospective and are not
  labeled as verified point-in-time prices.
- Existing provider limitations remain: unresolved current-season feed
  discrepancy, market/status semantics, exact publication-time meaning, quota
  headroom, and sustained multi-season live validation.
- The generic webhook mailer payload is an adapter contract, not confirmation
  of compatibility with a selected transactional email provider. Configure and
  test it before enabling real delivery.
- The frontend lockfile/build and database-backed auth flow must be verified in
  a trusted development environment before staging deployment.

## Safe operator commands

Offline verification:

```powershell
./scripts/verify-phase4.ps1
```

With a dedicated disposable PostgreSQL database:

```powershell
$env:PHASE4_DATABASE_URL = 'postgres://vfl:<local-password>@localhost:5432/vfl_phase4?sslmode=disable'
./scripts/verify-phase4.ps1 -DatabaseUrl $env:PHASE4_DATABASE_URL
```

Compose configuration only:

```powershell
$env:POSTGRES_PASSWORD = '<local-only-password>'
$env:INTERNAL_API_TOKEN = '<local-only-token>'
docker compose -f infra/docker/docker-compose.yml config
```

Before any deployment, create an untracked environment file with an explicit
authorized email, `APP_BASE_URL`, production HTTPS/cookie settings, a real
mailer configuration, and database/provider secrets. Keep PostgreSQL, workers,
model services, and operational endpoints on private networks; expose only a
TLS-terminated frontend entry point.

## Next-phase plan

1. Obtain the environment's trusted npm CA configuration, generate and review
   `apps/web/package-lock.json`, then run frontend tests, typecheck, and build.
2. Run the non-destructive and disposable-DB authentication integration suite,
   including concurrent token consumption, replay, CSRF, revocation, and
   session expiry.
3. Validate the selected email provider's webhook contract in a non-production
   environment without exposing tokens in logs.
4. Validate the OpenAPI document with the repository's contract tooling and
   perform an authenticated frontend-to-Go API smoke test against a private
   Compose environment.
5. Only after those gates, prepare a staging deployment and backup/restore
   rehearsal. Do not enable public access or automated betting.
