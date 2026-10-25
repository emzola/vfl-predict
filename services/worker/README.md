# Background workers

Phase 3 provides independent worker commands:

- `cmd/live-monitor` runs the leased live season/fixture/odds monitor with the
  shared provider request budget and adaptive polling plan.
- `cmd/prediction-worker` performs provisional or explicitly frozen round
  generation using the Phase 2 statistical baselines.
- `cmd/forward-settle` settles frozen predictions and records forward paper
  metrics.

All commands use PostgreSQL migrations and stop cleanly on SIGINT/SIGTERM.
No worker places bets or exposes a public API.

## Phase 3.6 controlled validation

Inspect the live-monitor lease before starting a worker. The inspector is
read-only by default and will never remove a valid lease:

```powershell
$env:PHASE36_DATABASE_URL = $env:DATABASE_URL
go run ./cmd/phase36-lease -database-url $env:PHASE36_DATABASE_URL
```

If the inspector reports an expired lease and a separate process check confirms
that the owner is not running, reclaim it atomically:

```powershell
go run ./cmd/phase36-lease -database-url $env:PHASE36_DATABASE_URL -reclaim-expired
```

If the lease row was already removed but old `running` job records remain,
the explicit reconciliation command marks only records older than four
minutes as cancelled and only when no valid lease exists:

```powershell
go run ./cmd/phase36-lease -database-url $env:PHASE36_DATABASE_URL -reconcile-orphaned-jobs
```

Run the bounded live cycle only after confirming provider authorization. It
uses the shared database request gate and enforces at least ten seconds between
upstream requests:

```powershell
$env:BET9JA_MAX_RETRIES = '0'
go run ./cmd/live-monitor -database-url $env:PHASE36_DATABASE_URL -allow-network -run-once -request-spacing-seconds 10
go run ./cmd/phase36-report -database-url $env:PHASE36_DATABASE_URL -report tmp/phase36-forward-report.json
```

For sustained observation, run the existing `cmd/live-monitor` process under a
service supervisor and stop it with SIGINT/SIGTERM. Do not shorten the
adaptive polling intervals or start duplicate owners. `scripts/verify-phase36.ps1`
is offline by default; `-RunLiveCycle` is an explicit network opt-in.
