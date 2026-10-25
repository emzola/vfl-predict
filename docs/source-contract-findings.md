# Phase 0 source contract findings

## Evidence inspected

The supplied endpoint document was extracted from `bet9ja endoints.docx` using the bundled `python-docx` runtime. Its table lists the same seven endpoint families described by the engineering specification: season discovery, fixtures/results, season metadata, round odds, team position history, and live table. No live request was made. The bundled DOCX-to-PNG renderer could not run because LibreOffice is not installed on the execution host; the document text and table content were still inspected read-only.

The seven supplied JSON payloads were copied verbatim to `fixtures/provider/` with stable names. They contain no credential-like fields in the credential scan used for this Phase 0 import.

## Observed payload inventory

| Fixture | `queryUrl` | Observed shape | Contract consequence |
|---|---|---|---|
| Historical seasons | `uniquetournament_seasons/14149` | 169 seasons; one `current` season, `3149336` | `14149` is a tournament ID, not a season ID; do not infer current season from another response |
| Completed fixtures | `stats_season_fixtures2/3149311` | 240 matches, 30 rounds × 8, 16 team UIDs, non-null scores | Golden fixture shape is 30×8 and 240 unique match IDs |
| Current season metadata | `stats_season_meta/3149369` | Season `_id` `3149369`, `currentseasonid` `3149336` | Requested season and current-season field disagree; retain both and fail safe |
| Upcoming fixtures | `stats_season_fixtures2/3149369` | 240 matches; 160 scored, 80 null-score; future `periods` is `[]` | Null scores remain null; `periods` must accept object or array |
| Historical odds | `stats_season_odds2/vf:season:3149336/23` | 8 match records, 346 markets, 1,116 outcomes; source timestamp in milliseconds | Timestamp is retained separately; market identity includes specifiers and outcome ID |
| Team position history | `stats_season_teampositionhistory/3149311/276510` | 16 teams, 30 rounds, no inference about other team-stat endpoints | Treat as optional enrichment until its full contract is validated |
| Live table | `vfl_tournament_livetablebyseasonandround/3149369/28` | 16 table rows; returned `currentround` is 27 | The requested round and returned current round are not interchangeable |

## Important discrepancies

1. The endpoint DOCX describes historical odds with season `3149311`, while the supplied odds JSON has `queryUrl` season `3149336`. The odds match IDs do not overlap either supplied fixture sample (`3149311` or `3149369`), so Phase 0 does not fabricate a join.
2. Fixture `_id` and `_seasonid` values are numeric in the fixture payload, while season `_id` values are strings. The typed boundary accepts observed string-or-integer season identifiers and canonicalizes them to `SeasonID` text.
3. The current fixture payload has `periods: []` for 80 null-score matches but an object with `p1` and `ft` for scored matches. The parser preserves this distinction.
4. The odds payload contains one `0.00` odds value on an inactive outcome. Raw schema acceptance and normalized odds eligibility are therefore separate: the parser retains the outcome and emits one validation issue; usable odds require a positive decimal.
5. The odds payload contains `market.specifiers: null` for 80 markets and strings for the remainder. Market ID alone is not a unique market identity.
6. The completed fixture sample returns `status: null` and `inlivescore: true` even when scores are present. These fields are retained but not treated as authoritative completion evidence.

## Phase 0 implementation boundary

The repository validates only the supplied payloads. The configured provider URLs are documentation/configuration values, and `BET9JA_CONNECTIVITY_ENABLED` defaults to `false`. Connectivity, request encoding, authentication/session behavior, and provider semantics require an authorized validation run described in [integration-test-plan.md](integration-test-plan.md).
