# Phase 4.1 verification report

Date: 2026-10-25  
Scope: frontend verification, authentication integration, dashboard contract
validation, private Docker Compose smoke testing, and security review

## Result

Phase 4.1 executable gates pass in the isolated test environment. The
frontend installs reproducibly with `npm ci`, frontend tests/typechecking and
the production build pass, the OpenAPI document parses, PostgreSQL-backed
authentication and dashboard integration tests pass, and the private Compose
smoke test passes through restart recovery.

This is not a production-readiness claim. Production email delivery, HTTPS
termination, real provider-backed dashboard data, sustained live operation,
VPS firewall/backup controls, and browser-driven visual E2E remain deployment
gates.

## Implemented

- Added the reproducible `apps/web/package-lock.json` and OpenAPI validator
  script (`npm run openapi:validate`).
- Resolved the local npm certificate issue with a verified, ignored CA file
  supplied through npm's `cafile` option. `strict-ssl` remains enabled; no
  TLS bypass or credential was added.
- Updated the frontend build dependencies to Next 16.4.3, Vitest 5.0.5, Vite
  6.4.5, and PostCSS 8.5.31. The production build uses the supported Webpack
  path for deterministic Windows/CI verification. The old `middleware.ts`
  convention was migrated to `proxy.ts` without changing the dashboard route
  guard.
- Added security headers and production fail-closed mailer behavior in the Go
  API.
- Added guarded PostgreSQL authentication integration coverage and deterministic
  dashboard API integration coverage. Mutating tests require both an explicit
  database URL and `PHASE4_ALLOW_MUTATING_TESTS=1`.
- Added `scripts/verify-phase41.ps1`, which runs the frontend, OpenAPI, Go,
  prior-phase, PostgreSQL, and optional isolated Compose gates.
- Corrected the Compose API command so the existing Docker entrypoint receives
  `-listen 0.0.0.0:8090`; the previous duplicated executable argument caused
  the API to bind only to container loopback and made the web proxy return
  HTTP 500.

## Verification evidence

The final verification used a disposable PostgreSQL database at
`127.0.0.1:55433` and a separate Compose project with web port `3301` and
PostgreSQL port `55434`. The Compose project was removed with its volume after
the smoke run.

Passed checks:

- `npm ci --no-audit --no-fund --cafile=<ignored managed CA>`.
- `npm test`: 1 test file, 1 test passed under Vitest 5.0.5.
- `npm run typecheck`.
- `npm run openapi:validate`: `packages/contracts/openapi.yaml` valid under
  `@apidevtools/swagger-parser`.
- `npm run build`: successful Next production build with all dashboard routes.
- `go vet ./...` and `go test -p 1 -count=1 ./...`.
- Existing Phase 0–3.6 verification scripts, with their documented offline
  skips and zero provider requests.
- PostgreSQL auth integration: token issuance/consumption, generic
  unauthorized handling, expiry, replay, concurrent redemption, sessions,
  revocation, logout, CSRF, rate limiting, audit records, and protected API
  access.
- Deterministic dashboard API integration across overview, season/round,
  prediction, future-round, history, match, odds, performance, paper-testing,
  operations, settings, and session resources.
- Private Compose smoke: PostgreSQL migrations, API/web startup, web-to-API
  health HTTP 200, unauthenticated dashboard redirect HTTP 307, no
  host-published API port, loopback-only PostgreSQL port, and API/web restart
  recovery.

The frontend audit reports zero critical and zero high advisories. Three
moderate dev-only Tailwind/PostCSS-parser advisories remain; the available fix
requires a Tailwind 4 migration and is recorded in the assumptions register.

## Secure npm setup

The managed CA file used for this workstation is intentionally outside source
control (`tmp/` and `*.pem` are ignored). Operators should use their own
trusted CA path only when the environment requires it:

```powershell
cd apps/web
npm config get strict-ssl       # must be true
npm ci --no-audit --no-fund --cafile C:\secure\managed-root.pem
npm test
npm run typecheck
npm run build
```

Do not use `strict-ssl=false` or `NODE_TLS_REJECT_UNAUTHORIZED=0`.

## Authentication and dashboard coverage

The full login flow is covered at the Go HTTP integration boundary using the
development-only in-memory mail capture: request, link extraction, atomic
verification, secure session creation, protected dashboard access, logout,
replay/expiry rejection, and session revocation. No real email was sent.

The deterministic dashboard test inserts clearly synthetic records into the
disposable database and validates response shapes and non-empty data for every
dashboard resource. It does not represent genuine predictions or provider
observations.

## Remaining unverified items

- The real transactional email provider's webhook/API payload and delivery
  behavior are not validated.
- A browser automation/visual E2E run was not performed; the authenticated
  HTTP flow and frontend production build are covered instead.
- No Bet9ja request was made during Phase 4.1, and no live provider-backed
  dashboard dataset was used.
- Production HTTPS/TLS, reverse-proxy headers/trust boundaries, firewall
  rules, secret injection, backups, monitoring, and Contabo deployment remain
  operator tasks.
- The runtime continues to preserve the earlier distinction between
  retrospective odds and genuinely pre-cutoff observed odds; this phase adds
  no evidence that historical prices were available at historical cutoffs.

## Next phase plan

1. Select and validate the actual transactional email provider in a non-
   production environment, including delivery, expiry, replay, and log
   redaction checks.
2. Run browser-driven authenticated smoke tests against a seeded local stack
   and review responsive/accessibility behavior.
3. Prepare the production reverse proxy, HTTPS certificate automation, secret
   injection, backups, and firewall policy without deploying yet.
4. Revisit the moderate Tailwind/PostCSS-parser advisories as a planned
   frontend dependency migration, then perform a final production security
   review.
5. Only after those gates pass, schedule a separate controlled deployment
   rehearsal; do not enable public access or automated betting.
