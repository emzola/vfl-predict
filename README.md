# Bet9ja VFL

Private, auditable Bet9ja Virtual Football League analytics platform.

This checkout contains Phase 0 contract work, Phase 1 live-contract/PostgreSQL/conservative ingestion, Phase 2 deterministic research baselines, Phase 3 controlled live monitoring/prediction lifecycle, Phase 3.5 LightGBM research comparison, the Phase 4 private authenticated dashboard boundary, and Phase 5 production deployment preparation. It does not contain public registration, payments, or automated betting.

## Repository layout

```text
apps/web/                  Next.js private dashboard and magic-link client
services/api/              Go internal-only operational API
services/provider/         Phase 0 provider contracts and parsers
services/ingest/           Phase 1 rate-limited client, persistence, checkpoints, and coverage
services/research/         Phase 2 point-in-time features, baselines, backtests, and research-only betting analysis
services/live/             Adaptive live season/fixture/odds monitor
services/prediction/       Transactional provisional/frozen prediction engine
services/settlement/       Forward paper settlement and metrics
services/worker/           Worker command documentation
services/model/             Python provider contracts plus the Phase 3.5 native LightGBM research service
cmd/historical-ingest/     Bounded operator-controlled historical importer
cmd/phase1-verify/         Optional migration verification helper
cmd/phase2-evaluate/       Offline database feature/backtest/report tool
cmd/phase2-verify/         Synthetic offline Phase 2 verification
cmd/live-monitor/           Independent live feed worker
cmd/prediction-worker/      Independent prediction worker
cmd/forward-settle/         Forward paper settlement command
cmd/ops-api/                Loopback-only operational API
cmd/phase3-verify/          Offline Phase 3 verification
cmd/phase35-export/         Export canonical features and labeled odds for Python research
cmd/phase35-register/       Register checksummed research artifacts and experiments as candidates
cmd/phase35-verify/         Verify Phase 3.5 report, artifact checksums, and registry state
packages/contracts/        JSON Schema and OpenAPI contracts
fixtures/provider/         Seven copied owner-supplied JSON payloads
infra/docker/              Development and production Compose environments
infra/systemd/              Lightweight production host health timer
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

Run the Phase 2 offline verification (no provider requests):

```powershell
./scripts/verify-phase2.ps1
```

Run the Phase 3 offline verification (no provider requests):

```powershell
./scripts/verify-phase3.ps1
```

Run the Phase 3.5 offline verification (no provider requests):

```powershell
./scripts/verify-phase35.ps1
```

Run the Phase 4 verification (no provider requests):

```powershell
./scripts/verify-phase4.ps1
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

Compose starts PostgreSQL, a private Go API, and a loopback-published Next.js
frontend. Historical ingestion and the independent workers remain explicit
operator commands. Do not put real provider credentials, email-provider
credentials, cookies, or tokens in this repository.

## Historical ingestion operator commands

After PostgreSQL is running and `DATABASE_URL` is set, a single completed
season can be imported with:

```powershell
go run ./cmd/historical-ingest -allow-network -season-id 3149425 -max-seasons 1 -report tmp/phase1-3149425.json
```

The command uses a shared ten-second minimum request spacing, bounded retries,
cache validators, raw payload retention, checkpoints, and quarantine records.
It is not run automatically and does not perform an unrestricted historical
backfill. Automatic selection reuses seasons with complete 30-round fixture and
odds-match coverage. Run a dry run first; a bounded two-season batch is:

```powershell
go run ./cmd/historical-ingest -allow-network -max-seasons 2 -max-retries 1 -report tmp/phase2-controlled-batch.json
```

The maximum is bounded at thirty seasons per invocation, and the default
remains one. A controlled Phase 3 expansion can be explicitly selected with
`-max-seasons 20` or another operator-chosen value up to 30; it is never run
automatically.

## Phase 3 worker commands

Live collection remains an explicit, authorized operation:

```powershell
go run ./cmd/live-monitor -allow-network -run-once
go run ./cmd/prediction-worker -run-once -season-id <season> -round <round> -freeze
go run ./cmd/forward-settle
go run ./cmd/ops-api
```

The live monitor and historical importer share the provider client's global
request budget. Network operation requires the explicit connectivity flag and
keeps the ten-second minimum spacing. The operational API binds to loopback by
default; set an internal token before using a non-loopback bind.

The prediction worker defaults to the `bookmaker_first` production policy. For
each fixture it uses normalized bookmaker-implied 1X2 probabilities only when a
complete verified line was observed before the cutoff; otherwise it records a
per-fixture Poisson fallback. Use `-model poisson` to explicitly run the
statistical fallback policy.

## Phase 2 research evaluation

After importing a controlled batch, run the deterministic research evaluator:

```powershell
go run ./cmd/phase2-evaluate -database-url $env:DATABASE_URL -report tmp/phase2-report.json
```

Historical odds returned after settlement are excluded from point-in-time
features by default. Use `-allow-retrospective-odds` only for a separately
labeled retrospective bookmaker comparison. Add `-persist` to store feature
snapshots, predictions, and evaluation metrics in the Phase 2 research tables.

## Phase 3.5 LightGBM research evaluation

The canonical Go loader exports a versioned dataset for the Python model
service. The explicit retrospective flag is required for the historical
bookmaker comparison because stored historical odds are not verified to have
been available at the original cutoff:

```powershell
$env:DATABASE_URL = 'postgres://vfl:<local-password>@localhost:<port>/vfl?sslmode=disable'
go run ./cmd/phase35-export -database-url $env:DATABASE_URL -allow-retrospective-odds -output tmp/phase35-dataset.json
python -m services.model.vfl_lgbm --dataset tmp/phase35-dataset.json --output-report tmp/phase35-lgbm-report.json --output-manifest tmp/phase35-manifest.json --artifacts-dir artifacts/models/phase35
go run ./cmd/phase35-register -database-url $env:DATABASE_URL -manifest tmp/phase35-manifest.json
go run ./cmd/phase35-verify -database-url $env:DATABASE_URL
```

The evaluator uses chronological season folds, reserves the newest seasons as
an untouched final test block, fits calibration and ensemble weights only on
development validation rows, and never promotes an artifact automatically.
Use `scripts/validate-phase35-live.ps1 -AllowNetwork` only for an explicitly
authorized, bounded live read-only cycle; it keeps the ten-second request
spacing and respects the existing worker lease.

## Contract boundary

The supplied payloads remain golden evidence, while the Phase 1 and Phase 2 reports record
the limited read-only live probes. Historical odds remain retrospective rather
than verified point-in-time snapshots; only odds observed by this application
before a stored cutoff can enter the forward prediction/paper-testing track.
See [docs/phase-1-report.md](docs/phase-1-report.md),
[docs/phase-2-report.md](docs/phase-2-report.md),
[docs/phase-3-report.md](docs/phase-3-report.md),
[docs/phase-4-report.md](docs/phase-4-report.md),
[docs/api-assumptions.md](docs/api-assumptions.md), and
[docs/integration-test-plan.md](docs/integration-test-plan.md).
