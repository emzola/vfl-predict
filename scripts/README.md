# Operational scripts

`verify-phase0.ps1` runs the Phase 0 offline checks.

`verify-phase1.ps1` runs Go/Python tests, formatting/vetting, Compose parsing,
and the optional PostgreSQL integration test. It does not contact Bet9ja.

The bounded historical importer is the Go command
`go run ./cmd/historical-ingest`. It requires a database and an explicit
`-allow-network` flag for provider requests. It defaults to one completed
season and caps a run at ten seasons.
