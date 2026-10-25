# Python model service

The model service contains provider contract tests plus the Phase 3.5 native
LightGBM research pipeline. Install the declared runtime dependencies from this
directory when setting up a separate environment:

```powershell
python -m pip install -e services/model
```

The service does not contact Bet9ja. Export data through the canonical Go
loader, then run the chronological evaluator from the repository root:

```powershell
go run ./cmd/phase35-export -database-url $env:DATABASE_URL -allow-retrospective-odds
python -m services.model.vfl_lgbm
```

The historical odds flag is explicit because returned-after-settlement odds
are not verified point-in-time snapshots. LightGBM artifacts are written under
`artifacts/` (ignored by Git), and registration stores checksums and metadata
as `candidate` records only.
