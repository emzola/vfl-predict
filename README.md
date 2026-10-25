# Bet9ja VFL

Private, auditable Bet9ja Virtual Football League analytics platform.

This checkout is intentionally limited to Phase 0 of the engineering specification. It contains the supplied provider payloads as golden fixtures, source-contract schemas, canonical identifiers, fixture and odds parsing tests, and a PostgreSQL development container. It does not contain prediction algorithms, a live provider adapter, authentication, migrations, or a claim of provider connectivity.

## Repository layout

```text
apps/web/                  Next.js dashboard placeholder
services/api/              Go API placeholder
services/provider/         Phase 0 provider contracts and parsers
services/worker/           Worker placeholder
services/model/            Python contract types only; no modeling code
packages/contracts/        JSON Schema and OpenAPI contracts
fixtures/provider/         Seven copied owner-supplied JSON payloads
infra/docker/              Development Compose environment
docs/                      ADRs, source findings, assumptions, test plan
```

## Phase 0 commands

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

Validate the Compose file using local-only placeholder values:

```powershell
$env:POSTGRES_PASSWORD = 'replace-me'
docker compose -f infra/docker/docker-compose.yml config
docker compose -f infra/docker/docker-compose.yml up -d postgres
docker compose -f infra/docker/docker-compose.yml down
```

The Compose file starts PostgreSQL only. Database migrations and application services are deliberately deferred to Phase 1 and Phase 2. Do not put real provider credentials, SMTP credentials, cookies, or tokens in this repository.

## Contract boundary

The payloads are evidence from supplied samples, not proof that the documented URLs are callable from this environment. Provider connectivity is disabled by default. See [docs/source-contract-findings.md](docs/source-contract-findings.md), [docs/api-assumptions.md](docs/api-assumptions.md), and [docs/integration-test-plan.md](docs/integration-test-plan.md) before implementing a live adapter.
