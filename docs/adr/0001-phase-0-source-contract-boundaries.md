# ADR 0001 Phase 0 source contract boundaries

## Status

Accepted for Phase 0 scaffolding. Live-provider behavior remains unvalidated.

## Decision

Use a tolerant raw-provider boundary and a strict canonical normalization boundary.

The raw schemas retain provider field names, nullable values, unknown fields, raw JSON payloads, provider timestamps, and provider settlement indicators. The Go normalizer then validates canonical namespaces, round bounds, UTC epoch conversion, team and match joins, decimal odds, and market identity. Invalid odds are retained as raw outcomes and emitted as validation issues so ingestion can quarantine them without silently dropping a record.

The provider's textual namespaces are not used as database keys directly:

| Provider value | Canonical value | Raw value retained |
|---|---|---|
| `vf:match:1412174996` | `MatchID(1412174996)` | yes |
| `sr:competitor:276515` | `TeamUID(276515)` | yes |
| `vf:tournament:14149` | `TournamentID(14149)` | yes |
| season `_id` string or integer | `SeasonID` string | yes |
| odds `"1.75"` | validated `Decimal` text | yes |

Fixture status is fail-safe. A non-null result alone is not completion; the caller must supply a current time and settlement confirmation window. A provider `status` value is retained but not interpreted because the supplied completed fixture sample has `status: null` and `inlivescore: true`.

## Consequences

- Phase 0 can test real supplied payloads without claiming API connectivity.
- Provider drift is visible through parser errors and validation issues.
- No prediction algorithm, database migration, authentication flow, or live adapter is included.
- The live adapter must validate URL transport, caching, rate limits, timestamps, joins, and market settlement rules before it is enabled.
