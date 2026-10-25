# Bet9ja VFL

Private, auditable Bet9ja Virtual Football League analytics platform.

This checkout contains Phase 0 contract work and the Phase 1 live-contract, PostgreSQL, and conservative historical-ingestion boundary. It does not contain prediction algorithms, model training, dashboard features, authentication, or live prediction scheduling.

## Repository layout

```text
apps/web/                  Next.js dashboard placeholder
services/api/              Go API placeholder
services/provider/         Phase 0 provider contracts and parsers
services/ingest/           Phase 1 rate-limited client, persistence, checkpoints, and coverage
services/worker/           Worker placeholder
services/model/            Python contract types only; no modeling code
cmd/historical-ingest/     Bounded operator-controlled historical importer
cmd/phase1-verify/         Optional migration verification helper
packages/contracts/        JSON Schema and OpenAPI contracts
fixtures/provider/         Seven copied owner-supplied JSON payloads
infra/docker/              Development Compose environment
db/migrations/             PostgreSQL schema migrations
docs/                      ADRs, source findings, assumptions, and phase reports
```

## Verification commands

Run the Go contract and golden tests from the repository root:

```powershell
go test ./...
```

Run the Python contract checks without installing project dependencies:

```powershell
python -m unittest discover -s services/model/tests -p 'test_*.py'
```

Run the full local Phase 0 verification (formatting, vetting, tests, Python compilation, and Compose parsing):

```powershell
./scripts/verify-phase0.ps1
```

Run the Phase 1 offline verification:

```powershell
./scripts/verify-phase1.ps1
```

Run the optional PostgreSQL integration test by setting
`PHASE1_DATABASE_URL` to a dedicated local database before invoking the same
script. The test is skipped when the variable is absent.

Validate the Compose file using local-only placeholder values:

```powershell
$env:POSTGRES_PASSWORD = 'replace-me'
docker compose -f infra/docker/docker-compose.yml config
docker compose -f infra/docker/docker-compose.yml up -d postgres
docker compose -f infra/docker/docker-compose.yml down
```

The Compose file starts PostgreSQL only. Phase 1 has no always-on application
service; historical ingestion is an explicit operator command. Do not put real
provider credentials, SMTP credentials, cookies, or tokens in this repository.

## Phase 1 operator command

After PostgreSQL is running and `DATABASE_URL` is set, a single completed
season can be imported with:

```powershell
go run ./cmd/historical-ingest -allow-network -season-id 3149425 -max-seasons 1 -report tmp/phase1-3149425.json
```

The command uses a shared ten-second minimum request spacing, bounded retries,
cache validators, raw payload retention, checkpoints, and quarantine records.
It is not run automatically and does not perform an unrestricted historical
backfill.

## Contract boundary

The supplied payloads remain golden evidence, while the Phase 1 report records
the limited read-only live probe. Provider connectivity is still disabled by
default, and historical odds remain retrospective rather than verified
point-in-time snapshots. See [docs/phase-1-report.md](docs/phase-1-report.md),
[docs/api-assumptions.md](docs/api-assumptions.md), and
[docs/integration-test-plan.md](docs/integration-test-plan.md).
