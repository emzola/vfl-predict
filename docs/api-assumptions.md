# Unresolved provider API assumptions

This register is the Phase 0 source of truth for questions that must be resolved before enabling ingestion. “Observed” means present in an owner-supplied document or sample; it does not mean verified against a live provider.

| ID | Unresolved assumption or question | Current evidence | Safe Phase 0 handling | Required validation |
|---|---|---|---|---|
| API-01 | How is the literal `?/` URL segment transported, and are signed/session-dependent components required? | It appears in both DOCX URLs and the specification templates. | Keep the complete base URL configurable; make no request. | Authorized request with exact URL encoding and redacted transport trace. |
| API-02 | What are the provider rate limits, acceptable polling cadence, CDN cache semantics, and cache-key rules? | `_maxage` values and refresh schedules appear in samples/specification, but no limit was measured. | Use disabled connectivity and conservative configurable defaults. | Measure 429/5xx behavior, `Retry-After`, `Cache-Control`, `_maxage`, ETag, and Last-Modified. |
| API-03 | Does the odds `timestamp` represent publication, response generation, cache time, or another event? | The sample has one outer millisecond timestamp and no per-price timestamp. | Store it as `source_timestamp_millis`; do not call it a publication time. | Poll an upcoming round and compare provider timestamp, fetch time, response headers, and price changes. |
| API-04 | What do market `status` values, outcome `active`, and `result=-1` mean, including suspension and correction behavior? | The sample has status 0/1, inactive outcomes, and `result=-1` throughout. | Preserve all raw indicators; exclude invalid odds from usable values; implement no settlement rules. | Obtain provider semantics and test active/suspended/settled/void/corrected examples. |
| API-05 | Are all 169 listed seasons retained, is the list paginated, and can older seasons be requested? | The sample reports 169 seasons and `_pages` is observed on fixture responses. | Do not infer retention or pagination beyond the supplied snapshot. | Query oldest IDs, pagination controls, retention boundary, and duplicate behavior. |
| API-06 | Are team UIDs and match IDs stable across seasons and across fixture/odds/live feed variants? | Namespaces are consistent within individual samples; the supplied odds IDs do not join the fixture samples. | Require explicit namespaced parsing and retain raw IDs; no cross-sample join is claimed. | Join completed and upcoming fixture/odds responses from the same season and round. |
| API-07 | Which timezone, deployment domain, email provider, VPS sizing, and storage budget are authoritative? | URLs include `America:Los_Angeles` and `Europe:Berlin`; payloads include provider timezone labels and UTC epoch seconds. | Normalize `uts` to UTC; defer display/deployment choices. | Owner selects display timezone/domain/email/storage and verifies DST presentation. |
| API-08 | What commercial reuse, attribution, and redistribution terms apply to feed data? | Owner states authorization exists, but terms are outside the repository. | Keep the app private and record no authorization secrets or legal claims. | Store the owner's authorization record outside source control and review before deployment. |
| API-09 | What are the exact settlement rules for Asian handicap, composite, halftime/fulltime, odd/even, and provider-specific markets? | Many such markets appear in the sample; no rules document was supplied. | Preserve market ID/name/specifiers/outcomes but do not settle or model them. | Capture documented rules and build market-specific settlement fixtures. |
| API-10 | Are odds for every future round available early enough for the pre-cutoff policy? | Owner states historical odds are retrievable; the timing of future-round availability is unverified. | No prediction dispatcher or cutoff claim exists in Phase 0. | Poll future rounds under authorization and record first availability, changes, closure, and cutoff. |
| API-11 | Is `currentseasonid` authoritative when it differs from the requested season or season-list `current` flag? | `stats_season_meta/3149369` returns `currentseasonid: 3149336`; the season list marks `3149336` current. | Retain requested and returned IDs; never replace the requested ID. | Cross-check season list, fixture window, metadata, and live table for several refreshes. |
| API-12 | Is `periods` contractually an object, an empty array, or another shape for unplayed fixtures? | Scored fixtures use `{p1,ft}`; 80 future fixtures use `[]`. | Typed parser accepts both and preserves `PeriodsKind`. | Query future, in-progress, postponed, and canceled fixtures and record shapes. |
| API-13 | Does `status: null` plus `inlivescore: true` mean live, completed, or a stale flag? | This exact combination appears in the completed sample. | Status is retained as raw JSON and completion is fail-safe. | Compare the same fixture before, during, after settlement and after cache refresh. |
| API-14 | Does the DOCX odds example use the wrong season ID, or is the supplied odds payload from another season? | DOCX says `3149311`; odds JSON query URL says `3149336`; IDs do not join supplied fixtures. | Treat the mismatch as an explicit non-joinable golden fixture condition. | Re-fetch a completed round using the exact season ID in the authorized request and verify joins. |
| API-15 | Are provider `year` values display years or internal identifiers? | Samples expose values such as `46879` as strings/integers, not human calendar years. | Preserve the raw value; do not use it for season ordering. | Compare provider year with start/end timestamps and displayed names across seasons. |
| API-16 | What are the semantics of `doc[0]._dob`, `_maxage`, and odds `timestamp` units? | `_dob` and fixture `uts` look like seconds; odds `timestamp` is millisecond-scale. | Parse fixture times as UTC seconds and odds source timestamp as UTC milliseconds; keep raw numbers. | Verify units against response headers and an authorized clock comparison. |
| API-17 | Can unknown fields, omitted fields, or type changes occur between endpoint variants? | Samples already show type variation in `periods`, IDs, nullable fields, and `year`. | Raw schemas allow unknown fields; required canonical fields fail parsing and are quarantined. | Run schema-drift probes against all endpoint families and archive failures. |
| API-18 | Does a market with invalid odds need to be omitted, quarantined, or represented as closed? | One inactive outcome has `odds: "0.00"`. | Retain raw outcome and validation issue; only positive decimals are usable. | Confirm provider meaning and observe whether invalid values recur or are replaced. |

No item above is resolved by the presence of a sample payload. Until the relevant validation is complete, the application must not claim API connectivity, historical point-in-time odds, settlement correctness, or predictive performance.

## Phase 1 live validation update (2026-10-25)

The following findings came from a small authorized, read-only probe of the documented public URLs. The probe used no credentials and was limited to season discovery, one completed season's fixtures, round 1 odds for that same season, and one current-season fixture response. These findings narrow the assumptions below; they do not validate every endpoint, retention boundary, settlement rule, or historical publication timestamp.

| ID | Verified or narrowed finding | Remaining limitation |
|---|---|---|
| API-01 | The literal `?/` segment and the documented `stats_season_fixtures2/{seasonId}` and `stats_season_odds2/vf:season:{seasonId}/{round}` paths returned HTTP 200 responses from this environment. An unsupported fixture path returned HTTP 200 with a provider error-shaped `data` object, so HTTP status alone is not sufficient. | Authorization/session requirements for other deployments and endpoint families remain unverified. |
| API-02 | Responses exposed `Cache-Control` and `Last-Modified`; the completed fixture response advertised `max-age=19` with `_maxage=20`, while the odds response advertised `max-age=2`. No ETag was observed in this probe. | Provider quotas, `Retry-After` behavior, and safe production polling cadence remain unverified. The implementation therefore uses a shared 10-second minimum request spacing and bounded retries. |
| API-03 / API-16 | The live odds response supplied an outer millisecond `timestamp`; the fixture response supplied `_dob` in seconds. The odds timestamp was retained as a source timestamp and was close to the provider document birth time. | This does not establish that either value is a price publication time, opening price time, or cutoff-observable time. |
| API-04 | In live round 1, all observed market statuses were `-3`, all outcome `active` values were `0`, and settled outcome `result` values were `0` or `1`. One outcome returned `odds: "0.00"`. | The meaning of status `-3`, inactive outcomes, corrections, voids, and market-specific settlement rules remains unverified. Invalid odds are retained and quarantined. |
| API-05 | Live season discovery returned 168 season records and identified current season `3149454`; the latest non-current completed candidate used for the probe was `3149425`. | This differs from the supplied 169-season sample and does not establish pagination or the full historical retention boundary. |
| API-06 / API-14 | Season `3149425` fixtures exposed numeric match IDs; round-1 odds exposed the same IDs as `vf:match:<numeric>`. All 8 odds records joined after removing the namespace, and the 16 fixture and odds team UID sets matched. | This validates one season/round join only. It does not prove stability across all seasons or endpoint variants. |
| API-10 | The historical odds feed returned 8 records and 1,118 outcomes for round 1 after all 8 fixtures had final scores, demonstrating that post-settlement historical odds are retrievable. | The response does not prove that the returned prices were available before the season or before any historical forecast cutoff. They are stored as `historical_returned_after_settlement`, never as verified point-in-time snapshots. |
| API-12 / API-13 | The completed probe had 240 final score pairs with `periods` objects; the current-season probe had 112 partial/null score pairs with `periods: []`. Completed fixtures still returned `status: null` and `inlivescore: true`. | The provider's authoritative live/completed status semantics remain unresolved; score/time plus the configured settlement window remains the fail-safe classifier. |
| API-17 | Live responses matched the supplied core envelope but also demonstrated a valid HTTP-200 provider error envelope for an unsupported path and live market status values not seen in the supplied odds sample. | Unknown-field/type drift across all endpoint families remains possible; raw payloads and validation failures are mandatory. |

The exact probe observations are summarized in [phase-1-report.md](phase-1-report.md). The original unresolved questions remain open unless the table explicitly narrows them.

## Phase 2 implementation findings and remaining assumptions (2026-10-25)

The Phase 2 research loader was exercised against the locally persisted,
controlled completed-season import. This is a normalization finding, not a
claim that the provider has published a stable public contract:

| ID | Finding or implementation policy | Remaining limitation |
|---|---|---|
| API-19 | In a persisted completed-season response, market `1` was named `1x2`; its outcome IDs were `1`, `2`, and `3`. The draw was identified by the outcome label `draw`, while the two team labels identified home/away. The research adapter therefore requires a complete three-way line and uses labels as a narrow fallback rather than assuming `X` or fixed numeric IDs. | The provider has not supplied a market catalog or a versioned outcome-ID contract. Every season/response must continue to be validated, and an incomplete or ambiguous 1X2 line is excluded from model/betting calculations. |
| API-20 | Persisted historical odds are usable as a retrospective post-settlement odds track and retain provider timestamp, HTTP observation/fetch time, observation kind, and `point_in_time_verified=false`. | No evidence establishes that these prices were available at a historical prediction cutoff. They cannot support a timestamp-faithful trading claim. |
| API-21 | Automatic historical ingestion skips a season only when the database has all 30 rounds, eight fixture rows per round, and eight joined odds match records per round. A completed job with quarantined invalid outcomes can be reused because raw payloads and quarantine rows remain auditable. | Coverage does not prove market settlement correctness, price completeness across every market, or original publication timing. |
| API-22 | Phase 2 round features use the earliest scheduled kickoff in a target round minus a 30-second safety buffer and exclude all same-round results. Verified odds are eligible only when an explicit observation time is at or before the cutoff. | The provider does not expose a verified historical market-availability timestamp, so persisted historical odds normally remain absent from point-in-time feature vectors. |

The Phase 2 code continues to preserve raw responses and validation failures;
it does not infer missing provider fields, convert retrospective odds into
verified snapshots, or enable unrestricted backfill by default.

## Phase 3 implementation policy and remaining validation (2026-10-25)

Phase 3 adds live collection and forward-paper plumbing. One bounded
authorized read-only live cycle and one bounded 20-season historical batch
were completed on 2026-10-25. These observations narrow some assumptions but
do not establish a complete provider contract or long-term refresh series:

| ID | Phase 3 policy or finding | Remaining limitation |
|---|---|---|
| API-23 | The live monitor selects exactly one discovery record with `current=true`; multiple or missing current records fail the cycle rather than silently guessing. Discovery rows are still persisted so season transitions remain auditable. | The provider's authoritative current-season signal and transition timing remain unverified across repeated live polls. |
| API-24 | Odds observed by this application while a fixture is pending are stored with `fetched_at`/`last_observed_at`, `observation_kind=live_observation`, and `point_in_time_verified=true`. The provider `timestamp` remains a separate source timestamp. | This flag proves local observation before the stored cutoff, not that the provider first published the price at that instant or that a market was executable. |
| API-25 | Live odds are normalized with the same canonical market/outcome adapter as historical odds. Price/validity/status changes are append-only in `odds_change_history`; identical cached responses do not create a new meaningful change. | Market status `-3`, inactive outcomes, corrections, and provider cache freshness semantics are still not explained by Bet9ja documentation. |
| API-26 | A round is publishable only when exactly eight same-round fixtures are present and every prediction row is inserted transactionally. Frozen rows cannot have probabilities, cutoff, odds references, or fixture identity mutated by SQL triggers. | Provider result correction behavior and non-1X2 settlement rules remain unresolved; forward settlement is limited to final scores and research-only 1X2 arithmetic. |
| API-27 | Operational endpoints are read-only and bound to loopback by default; non-loopback binding requires an internal token. No public authentication or dashboard surface is enabled. | Private-network deployment, reverse-proxy policy, and magic-link authentication are deferred. |
| API-28 | Historical backfill remains explicitly bounded at 30 seasons per invocation, defaults to one, reuses complete coverage, and keeps the shared ten-second minimum request spacing. An operator-authorized 20-season batch completed with 4,800 fixtures, 600 odds-round requests, 0 fixture/odds join failures, and 652 quarantined invalid outcomes; all selected seasons had 30 rounds of eight joined matches. | The batch used the importer’s in-process ten-second budget because it began before the persistent cross-process gate was added. Provider quota behavior and the full historical catalog remain unverified. |
| API-29 | In the controlled Phase 3 read-only cycle, the live feed identified current season `3149484`, while the historical season-list feed used by backfill identified `3149454` as current and selected it as an eligible completed candidate. The monitor preserves the live signal and does not substitute one ID for the other. | It is not yet known whether the two feed bases are intentionally on different clocks/deployments, or how operators should reconcile them for season transitions. A repeated cross-feed observation series is required. |
| API-30 | The bounded live cycle returned 23 market IDs, market `1` as the observed 1X2 line, 35 distinct specifier strings, market statuses from `-3` to `1`, active flags from `0` to `1`, provider results from `-1` to `1`, valid decimal odds from `1.00` to `100.00`, and one invalid `0.00` odds row. The database retained 3,348 normalized live odds rows, including 1,116 locally observed before kickoff and 2,232 post-kickoff or otherwise ineligible observations. | These values are observed payload characteristics, not provider semantics. The meaning of status `-3`, inactive outcomes, result values, corrections, and exact market availability remains undocumented. |

No Phase 3 implementation finding should be read as proof that retrospective
historical odds were available at an earlier decision cutoff. The forward track
uses only locally observed, cutoff-eligible snapshots; the historical research
track remains explicitly retrospective.

## Phase 3.5 research and live-validation update (2026-10-25)

Phase 3.5 used the existing 23 complete-season local dataset and did not
silently expand the provider import. The LightGBM comparison therefore adds no
new provider-contract evidence. Historical bookmaker rows were included only
through the explicit `retrospective_unverified_availability_explicit_opt_in`
export policy; they are not treated as contemporaneously available odds.

One additional `live-monitor -run-once` attempt was prepared with the required
10-second spacing and zero automatic retries. It did not make a new provider
request because the persistent `live-monitor` scheduler lease was held by
another owner. The worker was not force-stopped and the lease was not manually
released. The actual live observations summarized under API-29/API-30 remain
the bounded Phase 3 evidence, not a sustained Phase 3.5 refresh series.

The following remain unresolved and are explicit gates for any live model
promotion or executable paper-trading claim:

| ID | Phase 3.5 status | Required evidence |
|---|---|---|
| API-31 | A single live cycle cannot establish a refresh distribution, stable cache behavior, or provider quota headroom. | Collect a timestamped multi-cycle series through at least one upcoming round while preserving the shared request budget and `Retry-After` behavior. |
| API-32 | The live feed current season and historical feed current season still differ (`3149484` versus `3149454` in the prior bounded observation). Neither source is assumed authoritative. | Reconcile both feeds across repeated observations and a season transition, including fixture windows and metadata, before automatic transition handling. |
| API-33 | Market `1` was the observed 1X2 line in the bounded live cycle; statuses, `active`, and `result` values remain provider semantics rather than confirmed settlement rules. | Obtain provider market/status documentation and capture suspended, settled, void, and corrected examples before enabling additional markets. |
| API-34 | The current registry contains only `candidate` LightGBM artifacts; no live prediction was promoted or used to claim forward performance. | Establish genuinely pre-cutoff frozen predictions and settle them after final results, with locally observed odds provenance, across a sustained live sample. |

The runnable procedure is `scripts/validate-phase35-live.ps1`; it is offline
unless invoked with `-AllowNetwork`, requires a database URL, enforces the
10-second minimum spacing, and refuses to bypass a held worker lease.

## Phase 3.6 sustained-validation update (2026-10-25)

Phase 3.6 added a read-only lease inspector and an atomic expired-only reclaim
operation. The previous `live-monitor` lease was inspected before mutation:
owner `live-monitor-13772` had a database expiry at 10:05 local time while the
database clock was 10:36 and no corresponding worker process was present. It
was therefore classified as an orphaned expired lease and reclaimed only with
the `lease_until <= now()` database predicate. A later bounded attempt started
another monitor cycle, persisted a season-discovery observation, and then left
an unexpired lease/job row when the execution environment interrupted the
cycle. That lease was not bypassed. This is operational evidence that TTL
recovery is necessary; it is not evidence that a worker is still running.

| ID | Phase 3.6 finding | Remaining limitation |
|---|---|---|
| API-35 | The existing four-minute lease is owner/token scoped. Two workers cannot acquire the same unexpired key in the integration test; valid leases are not reclaimable, and expired leases can be reclaimed atomically. | A process terminated outside graceful shutdown can leave a `running` job record until operator recovery/TTL handling reconciles it. Sustained supervisor behavior remains to be validated. |
| API-36 | A bounded follow-up made a discovery request at 07:36:50Z, and a subsequent complete read-only cycle at 07:42:53Z–07:43:24Z fetched discovery, 240 fixtures, and odds for rounds 5 and 6 under the shared request budget. No provider HTTP errors were recorded. | This is still only a short observation window; it does not establish a refresh distribution, quota headroom across three seasons, or a sustained transition series. |
| API-37 | The live feed now identifies `3149569` as current, with `3149540` and `3149484` immediately preceding it in the same discovery response. The historical season-list evidence still reports `3149454` as current, so the cross-feed discrepancy is now `3149569` versus `3149454`. | Neither feed is assumed authoritative. Automatic season transition policy still requires repeated reconciliation across a transition and metadata/fixture windows. |
| API-38 | The current local operational database contains two provider-backed complete frozen rounds (`3149484`/14 and `3149569`/6), each with eight revisions frozen before kickoff and no provider-backed settlements. A separate late round-6 invocation completed at 07:44:50Z, six seconds after its 07:44:44Z cutoff, and correctly recorded `missed_cutoff` with zero revisions; it did not mutate the earlier immutable frozen run. | The other recorded forward settlements are synthetic integration evidence with no eligible odds; they are excluded from genuine forward claims. No three-season sustained observation was possible in this execution window. |
| API-38 | The Phase 3.6 report filters provider live observations from `phase3-test` rows and keeps provider response counts, cache reuse, odds observation provenance, job status, and round evidence separate. | A persisted response/observation count is not a wire-level request count when retries or cache reuse occur; provider quota/error behavior remains incompletely measured. |

The safe operator procedure is `services/worker/README.md` plus
`scripts/verify-phase36.ps1`. The script is offline by default. It does not
reclaim a lease unless `-ReclaimExpiredLease` is explicitly supplied, and it
does not make provider requests unless `-RunLiveCycle` is explicitly supplied.

## Phase 4 application-boundary update (2026-10-25)

Phase 4 adds an authenticated application boundary around persisted records;
it does not alter the provider contract or claim new provider connectivity. The
dashboard reads PostgreSQL through the Go API and never calls Bet9ja directly.

| ID | Phase 4 finding | Remaining limitation |
|---|---|---|
| API-39 | The application preserves the existing distinction between provider response timestamps, local observation timestamps, retrospective returned-after-settlement odds, and genuinely observed forward odds. | A dashboard timestamp is not evidence that a historical price was available at a past cutoff. Only locally captured pre-cutoff observations enter the forward paper-testing track. |
| API-40 | Authenticated routes, session storage, CSRF checks, and dashboard response shapes are implemented in the Go application boundary; operational routes remain internal-token/loopback protected. | No production email provider contract was supplied or validated. The configurable webhook mailer payload must be tested against the selected provider before enabling delivery. |
| API-41 | The Next.js frontend uses a same-origin API rewrite and performs no direct database or provider access; browser refreshes are bounded client reads. | Frontend dependency installation/build was not executable in this environment because npm could not validate the registry certificate and no lockfile was generated. The Docker build remains gated on obtaining a verified `package-lock.json`. |
| API-42 | Health and dashboard endpoints report stored freshness/provenance rather than fabricating missing predictions or forward performance. | Empty local databases, missing active models, missing odds provenance, and unresolved provider season/market semantics remain visible limitations rather than inferred values. |

These findings do not resolve API-02, API-03, API-04, API-06, API-10,
API-31, API-32, API-33, API-34, API-36, API-37, or API-38. They remain gates
for provider-backed claims and are not changed by the dashboard implementation.

## Phase 4.1 verification update (2026-10-25)

Phase 4.1 validated the application boundary against a disposable PostgreSQL
database and an isolated Docker Compose project. It did not make provider
requests, enable production email delivery, or validate a live Bet9ja feed.

| ID | Phase 4.1 finding | Remaining limitation |
|---|---|---|
| API-43 | The local npm environment is Node `22.12.0` / npm `10.9.2`, uses `https://registry.npmjs.org/`, and retains `strict-ssl=true`. Registry access succeeds when the environment's managed `AVG Web/Mail Shield Root` CA is supplied through an ignored local `cafile`; no TLS validation was disabled. | The CA is environment-specific and is not committed. CI/VPS operators must provide the correct corporate/system trust chain or use the normal OS/Node trust store. Never use `strict-ssl=false` or `NODE_TLS_REJECT_UNAUTHORIZED=0`. |
| API-44 | A disposable PostgreSQL run applying migrations 001–009 passed authorized/unauthorized generic login behavior, single-use and expired links, concurrent redemption, sessions, logout/revocation, CSRF, rate limiting, audit records, protected dashboard access, and deterministic dashboard API reads. | The test uses the development memory mailer and synthetic database fixtures. A real transactional email provider payload and delivery contract remain unvalidated. |
| API-45 | `packages/contracts/openapi.yaml` passes `@apidevtools/swagger-parser`; deterministic dashboard fixtures exercise overview, seasons/rounds, predictions, future rounds, history, match detail/odds, performance, paper testing, operations, settings, and sessions. | No browser-driven visual test or real provider-backed dashboard dataset was run in this environment. Frontend/API integration is validated at the HTTP contract and production-build boundaries. |
| API-46 | The Compose smoke test passed migrations, private API routing, health, login redirect, loopback-only PostgreSQL exposure, no host-published operational API, and API/web restart recovery. The API command was corrected to pass flags to its existing Docker entrypoint rather than repeat the executable path. | The smoke test uses local placeholder values and the development memory mailer. HTTPS termination, real email delivery, production secrets, firewall policy, backups, and VPS deployment remain unverified. |
| API-47 | Frontend audit after the patched Next 16.4.3/Vitest 5 dependency update reports zero critical and zero high advisories. | Three moderate dev-only Tailwind/PostCSS-parser advisories remain; their available fix is a Tailwind 4 migration and was not taken in Phase 4.1. They are not part of the standalone runtime image, but should be revisited before production deployment. |
| API-48 | The Next production build is configured to use Webpack and constrained Windows build concurrency because the local restricted environment produced intermittent child-process/parallel-directory `EPERM` failures with Next 16 Turbopack. The same build passed outside the restricted sandbox and in the Linux Docker image. | A native Windows build in a differently managed environment should be rechecked; the production target remains the Linux container. |

These findings do not resolve the provider-side assumptions listed above or
claim production readiness. They supersede the Phase 4 report's earlier
frontend/npm and PostgreSQL verification omissions.

## Phase 5 deployment-preparation update (2026-10-25)

Phase 5 adds deployment preparation only. No Contabo host, Cloudflare zone,
GHCR production package, or Resend account was accessed, and no live provider
request was made by this phase.

| ID | Phase 5 finding | Remaining limitation |
|---|---|---|
| API-49 | The existing `MagicLinkMailer` contract now has a Resend adapter. Its request envelope, bearer authorization handling, and failure behavior pass against an isolated local HTTP test server; production startup fails closed when HTTPS, secure cookies, the authorized email, or Resend configuration is missing. | A real Resend delivery and callback have not been completed. Sender-domain SPF/DKIM values must be copied from Resend into Cloudflare by the operator; no DNS values are invented here. |
| API-50 | Production Compose has one explicit `cmd/migrate` service. API/workers disable startup migrations, the PostgreSQL advisory lock remains authoritative, and `schema_migrations` records ordered filenames and SHA-256 checksums. A disposable Phase 4.1 database adopted all 11 records and passed a second idempotent run. | A production migration has not been applied. Any release changing `db/migrations/*.sql` is classified high risk and requires an explicit manual workflow confirmation. |
| API-51 | The application images build locally, image release tags are commit-SHA based, CI records digests, deployment uses SSH host-key verification plus a host `flock`, and rollback is image-only. | GHCR pull credentials, VPS health checks, worker transition behavior, and rollback compatibility remain unverified on the target host. |
| API-52 | Caddy configuration validates in the official container and filters magic-link query tokens/cookies from access logs. Only Caddy publishes ports 80/443 in production Compose. | Cloudflare Full (strict), origin certificate issuance/renewal, firewall allowlists, DNS, and actual HTTPS routing remain operator gates. |
| API-53 | The frontend lockfile now includes Playwright 1.55.1. Unit tests exclude browser suites; four deterministic login/dashboard/mobile browser tests pass locally. | The browser tests use local API fixtures and do not prove real email delivery, production authentication, or genuine forward prediction data. |
| API-54 | The production prediction default is now `bookmaker_first`: a complete, point-in-time-verified 1X2 line observed at or before the cutoff supplies normalized bookmaker probabilities; the fitted Go Poisson model is used per fixture when that line is incomplete, missing, or late. The selected engine, engine version, policy, and fallback reason are stored in each prediction payload. | The historical bookmaker comparison used returned-after-settlement odds and cannot establish forward availability. A sustained live sample is still required before treating the policy as evidence of a betting edge. |

These findings do not resolve the provider-side assumptions listed above,
especially the historical/live season discrepancy, refresh/quota behavior,
odds availability timing, or provider authorization terms. They also do not
claim that the application has been deployed or is profitable.
