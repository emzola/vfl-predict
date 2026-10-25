# Authorized provider integration test plan

This plan is intentionally not executed in Phase 0. It is the gate for a later adapter. Use a provider-authorized environment, a redacted request log, and a request budget approved by the owner.

## Preconditions

- Confirm commercial reuse and redistribution authorization outside the repository.
- Inject provider configuration through an untracked secret mechanism.
- Confirm the requested host, path prefix, language, timezone, and literal `?/` path behavior.
- Keep request/response bodies and headers redacted of cookies, tokens, authorization values, and personal data.

## Contract checks

1. Fetch the season list for tournament `14149`; count seasons, sort by `start.uts`, record `current` flags, and test the oldest available item.
2. Fetch a completed season fixture response; require 30 rounds × 8 fixtures, 240 unique match IDs, and 16 team UIDs. Compare fixture `_seasonid`, season `_id`, and tournament `_utid`.
3. Fetch an active/current season response; confirm future fixtures keep both result scores null and are not marked completed by the normalizer. Record `periods` shape for unplayed matches.
4. Fetch season metadata for the same current season; compare requested ID, season ID, `currentseasonid`, season-list current flag, and fixture time window.
5. Fetch odds for one completed and one upcoming round. Confirm eight odds records, fixture match-ID joins, team UID joins, decimal strings, market IDs, nullable specifiers, active/status values, and the outer timestamp.
6. Poll the same upcoming round twice under the approved cadence. Record whether prices, active flags, market status, source timestamp, cache headers, or response bodies change.
7. Fetch team position history and live table for a known season/round. Compare team identity and round semantics; do not infer that a requested live-table round equals its returned `currentround`.
8. Probe malformed and incomplete responses in a controlled test harness. Confirm the adapter quarantines them, records parser/schema version and content hash, and never silently drops records.
9. Confirm no provider credential appears in source files, fixtures, frontend bundles, structured logs, error messages, or test snapshots.

## Exit evidence

The adapter may be enabled only after the owner has a redacted request/response evidence bundle, a resolved assumption record, a coverage report, and passing integration tests for all checks above. Historical odds returned after settlement must remain labeled retrospective unless per-price publication timing is independently proven.
