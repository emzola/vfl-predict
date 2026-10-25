# Phase 0 implementation report

## Delivered

- Monorepo directories from the engineering specification, Git hygiene, placeholder configuration, and CI scaffolding.
- PostgreSQL 16 Docker Compose development environment bound to localhost; no provider or application service is enabled yet.
- Seven supplied JSON payloads copied to `fixtures/provider/` without credential-like fields.
- Go typed raw provider schemas, canonical IDs, UTC conversion, fail-safe fixture status classification, market identity, specifier normalization, and positive decimal validation.
- Python typed contract definitions using standard-library `TypedDict`/`NewType` only.
- JSON Schema fixtures and an internal OpenAPI Phase 0 boundary.
- Go golden tests covering completed fixture shape, future null scores, periods type drift, odds counts/IDs/specifiers/status preservation, decimal handling, timestamp units, and explicit non-joinability of the supplied odds sample.
- Source findings, an ADR, an unresolved API assumption register, and an authorized integration-test plan.

## Deliberate non-deliverables

No provider HTTP client, API connectivity, database migrations, prediction algorithm, model training, settlement engine, authentication, dashboard, or profitability claim was implemented.

## Verification commands

```powershell
go test ./...
go vet ./...
python -m unittest discover -s services/model/tests -p 'test_*.py'
python -m compileall -q services/model
$env:POSTGRES_PASSWORD = 'replace-me'
docker compose -f infra/docker/docker-compose.yml config
```

## Known limitations

The endpoint DOCX was text/table inspected with the bundled `python-docx` runtime, but visual rendering was unavailable because LibreOffice is not installed on the host. Live provider behavior is entirely unvalidated. The supplied odds sample is from query season `3149336` and does not join either supplied fixture sample, so the Phase 0 tests intentionally do not claim an odds-to-fixture join.

## Next phase

Phase 1 should add PostgreSQL migrations and a resumable, idempotent backfill CLI only after the authorized integration checks pass. It should import raw payloads and normalized seasons, teams, fixtures, and odds with quarantine and coverage reporting; preserve retrospective odds provenance; and keep live ingestion separate from historical backfill.
