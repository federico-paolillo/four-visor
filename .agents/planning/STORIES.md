# Story Specialist Findings

## Decomposition principles

- The stories below are proposals for the Coordinator. Their order is the proposed implementation order.
- Each story delivers a concrete reader or operator capability. Contract and storage foundations are kept only where they are necessary to make later vertical slices independently implementable and reviewable.
- Security, failure handling, observability, documentation, and unit or integration validation are part of the story that introduces the behavior. They are not deferred to cleanup stories.
- “Likely related architectural decisions” maps to the five proposed decisions in `.planning/ARCHITECTURE.md`; locked requirements and local implementation choices are not promoted into duplicate MADRs.
- Exact traceability uses the current `docs/SEED.md` section names and line spans. The quoted requirement summaries identify the precise clauses covered by each story.
- Every implementation handoff runs the applicable repository validation tasks: backend changes use `mise run be:build`, `mise run be:test`, and `mise run be:lint`; frontend changes use `mise run fe:build`, `mise run fe:test`, `mise run fe:lint`, and `mise run fe:typecheck`. Stories that touch both run both sets.
- Automated validation is limited to unit and integration tests. Smoke, end-to-end, and deployment tests remain excluded.

## Locked scope guardrails

All stories preserve the locked exclusions in `docs/SEED.md:2095-2377`. In particular, no story may introduce accounts, authentication, posting, moderation, preferences, search, bookmarks, read state, ranking, filtering, recommendations, client-triggered acquisition, on-demand resource fetching, incremental or partial lineage activation, lineage reconciliation, historical snapshots, backend media caching or proxying, persistent backend storage, multiple backend replicas, distributed coordination, a client-side router, SSR, non-Preact frameworks, non-Chrome browser support, Linux arm64, Kubernetes, smoke tests, end-to-end tests, deployment tests, or enterprise-grade availability. Canonical thread and post URLs remain 4chan URLs.

## Proposed stories

### US-001 — Run a configured and diagnosable backend health boundary

**Goal**

Provide the Operator with a small Go `net/http` service whose configuration, dependency health, and telemetry-export behavior are explicit before snapshot work is added.

**User value**

The Operator can start the backend, detect whether its two required runtime dependencies are usable, and diagnose failures without exposing infrastructure details or secrets.

**Scope**

- Load all Go application configuration exclusively from documented `FOURVISOR_` environment variables, applying SEED-defined defaults and rejecting invalid values at startup.
- Serve only `GET /health` at this stage, returning `200` when the process can respond, Memcached is reachable, and 4chan DNS resolves; otherwise return `503`.
- Keep the response body non-contractual and free of dependency details and secrets.
- Establish OpenTelemetry SDK/OTLP export for Go telemetry, inbound HTTP root spans, low-cardinality HTTP metrics, and structured error logging. Telemetry export failure must not fail health or request processing.
- Document environment variables, defaults, startup failures, health semantics, and the fact that no readiness endpoint exists.

**Out of scope**

- Snapshot routes, upstream HTTP acquisition, scheduling, lineage publication, Compose health checks, dashboards, alerting, or additional public backend endpoints.

**Dependencies**

- None.

**Likely related architectural decisions**

- None likely. The configuration source, OpenTelemetry topology, and health contract are locked; variable names, parsing, OTLP transport details, and resource naming are local implementation choices.

**Exact SEED traceability**

- `Full Requirements / Deployment` (`docs/SEED.md:191-210`): Go configuration uses only `FOURVISOR_`; health verifies backend responsiveness, Memcached, and 4chan DNS.
- `High-Level Architecture / HTTP routing` (`docs/SEED.md:375-394`): internal `GET /health`, `200`/`503`, non-contractual secret-free body, and no readiness or extra route.
- `Operational Flows / Backend component failure` and `/ Health check` (`docs/SEED.md:1027-1080`): required dependency failure fails the operation; health is intentionally shallow.
- `Deployment View / Failure model`, `/ Security`, and `/ Observability` (`docs/SEED.md:1266-1302`): telemetry is optional to application processing and diagnostics must respect the private network/security model.
- `Detailed Observability / OpenTelemetry`, `/ Tracing`, `/ Metrics`, `/ Logging`, and `/ Error handling` (`docs/SEED.md:1468-1604`): OTLP, HTTP roots, minimal metrics, meaningful logs, and failed spans.
- `Technology Stack / Backend` and `/ Configuration` (`docs/SEED.md:1771-1778`, `1852-1860`): Go, `net/http`, Memcached, OpenTelemetry, OTLP, and the environment prefix.

**Objective acceptance criteria**

1. With reachable Memcached and successful 4chan DNS resolution, `GET /health` returns `200`; either dependency failure produces `503` within a bounded request duration.
2. The response does not name dependency hosts, configuration values, cache keys, raw errors, or credentials.
3. Unsupported methods and undeclared routes are not treated as successful health requests; no readiness route exists.
4. Invalid or missing required `FOURVISOR_` values fail startup with a cause-preserving, secret-free diagnostic; SEED-defined defaults are documented and tested.
5. Every inbound request, including rejected methods/routes, creates an HTTP root span and updates low-cardinality request count/latency signals; errors mark the span and emit one meaningful structured error log, without routine begin/end logs.
6. An unavailable OTLP destination does not change the health result when required health dependencies remain available.

**Unit or integration validation**

- Unit-test configuration defaults, validation, and diagnostic redaction.
- Integration-test the handler with controllable Memcached reachability and DNS resolver seams, including cancellation and timeout behavior.
- Integration-test span status and metric attributes with an in-memory OpenTelemetry exporter, plus non-fatal exporter failure.

### US-002 — Enforce the exact snapshot version 1 contract at both boundaries

**Goal**

Give backend publication and browser synchronization one exact, fixture-backed interpretation of snapshot schema version 1.

**User value**

The Reader never activates structurally invalid or incompatible content, while valid upstream fields and ordering survive unchanged across the backend/browser boundary.

**Scope**

- Define backend JSON models/validation and frontend TypeScript parsing/validation for the exact nested schema version 1 contract.
- Validate ULID lineage identifiers and UTC RFC 3339 `observedAt` values.
- Enforce all wrapper fields, state/payload combinations, cardinality limits, and unknown-wrapper-field rejection.
- Preserve unrestricted board, page metadata, thread-summary, and post fields and values as opaque upstream objects.
- Maintain shared valid and invalid contract fixtures usable by Go and Vitest integration tests.
- Document that version 1 has no migration, adapter, compatibility window, route version, or partial acceptance.

**Out of scope**

- Upstream fetching, Memcached layout, HTTP serving, IndexedDB persistence, HTML sanitization, or a future schema version.

**Dependencies**

- None.

**Likely related architectural decisions**

- Proposed MADR 3, “Enforce snapshot version 1 with boundary validators and shared fixtures.” The exact schema itself is locked; the decision governs cross-language enforcement without restating the contract.

**Exact SEED traceability**

- `High-Level Architecture / Snapshot contents` (`docs/SEED.md:348-374`): lineage contents, failed/oversize representations, unchanged HTML/media references, and binary/user-state exclusions.
- `High-Level Architecture / Snapshot schema version 1` (`docs/SEED.md:429-495`): exact root, wrappers, opaque objects, ordering, cardinality, ULID/time validation, and version rejection semantics.
- `Locked Decisions / Snapshot model` and `/ Backend cache` (`docs/SEED.md:2115-2134`): exact version 1, no compatibility window, 250 limits, unchanged HTML, and binary exclusion.
- `Technology Stack / Data formats` (`docs/SEED.md:1792-1798`): JSON, unchanged HTML, and ULID identifiers.

**Objective acceptance criteria**

1. Both implementations accept the same canonical valid fixtures for present, failed, absent, and oversize resource combinations without changing opaque upstream fields or array order.
2. Both reject missing/extra wrapper fields, wrong types, invalid states, invalid state/payload combinations, invalid ULIDs, non-UTC timestamps, and catalog/thread cardinalities above 250.
3. `boards`, catalog, and thread failure wrappers contain no payload or failure-detail string; unexplained absence is represented only by the permitted missing optional field.
4. Schema versions other than integer `1`, including a missing version, are rejected explicitly.
5. Contract documentation matches the fixtures and states that no migration or fallback parsing exists.

**Unit or integration validation**

- Run table-driven Go unit tests and Vitest unit tests over the same valid/invalid fixture corpus.
- Add a cross-boundary integration fixture proving backend serialization is accepted by the frontend parser without losing opaque fields or ordering.

### US-003 — Observe every board and its first 250 catalog threads

**Goal**

Construct the board and catalog portion of a fresh lineage from scheduled 4chan observations while preserving upstream fidelity and bounded outbound behavior.

**User value**

The Reader can eventually see every observed board and the first 250 catalog entries in exactly the order and page boundaries returned by 4chan, including honest failure states.

**Scope**

- Implement the shared 4chan HTTP acquisition boundary with a single global rate limiter, default maximum concurrency of 10, five-second request timeout, selective bounded retry, context cancellation, and the required deployed-commit User-Agent.
- Fetch the board list and, when present, every observed board's catalog.
- Preserve board order, upstream values, catalog page order/boundaries, every page metadata field other than `threads`, and the first 250 thread summaries across pages.
- Represent a technical board-list failure as `boards.state = failed`; represent a technical catalog failure as `catalog.state = failed`; leave only genuinely unexplained missing resources absent.
- Start each construction with no resource reuse from a prior lineage.
- Instrument outbound requests as child spans and emit low-cardinality client request metrics and error logs without logging raw URLs, identifiers as metric labels, response bodies, or secrets.
- Document acquisition configuration and failure classification.

**Out of scope**

- Fetching thread bodies, Memcached publication, activation, the scheduler, background repair, automatic replay, or inferring why upstream content is absent.

**Dependencies**

- US-001.
- US-002.

**Likely related architectural decisions**

- None likely. Rate values, bounded retry/backoff, endpoint adapter boundaries, and opaque-object mechanics are story-level implementation/configuration details under locked acquisition semantics.

**Exact SEED traceability**

- `Full Requirements / Backend cache` and `/ Upstream acquisition` (`docs/SEED.md:108-116`, `168-179`): every board, first 250 catalog threads, global rate limit, concurrency 10, timeout, deadline-aware retries, and User-Agent.
- `High-Level Architecture / Upstream acquisition` (`docs/SEED.md:496-528`): scheduled-only acquisition policy, transient retries, fidelity, and no repair/inference.
- `Operational Flows / Board acquisition`, `/ Catalog acquisition`, and `/ Retry behavior` (`docs/SEED.md:707-732`, `733-752`, `780-811`): exact ordering, failed versus absent, Retry-After, and shared limits.
- `Design Notes / Immutable lineages` and `/ Upstream fidelity` (`docs/SEED.md:1334-1346`, `1374-1383`): from-scratch construction and unchanged upstream semantics.
- `Detailed Observability / Tracing`, `/ Metrics`, and `/ Logging` (`docs/SEED.md:1481-1587`): outbound spans, client metrics, sparse logs, and low-cardinality signals.

**Objective acceptance criteria**

1. A successful observation contains every board in upstream order and at most the first 250 thread summaries per catalog while preserving page order, page boundaries, metadata, and opaque values.
2. Every request attempt, including retries, passes through one process-wide limiter, never exceeds 10 concurrent outbound requests by default, times out after five seconds, carries `User-Agent: 4Visor/<deployed-commit-hash>`, and stops promptly on lineage cancellation.
3. Only network errors, timeouts, and rate limiting are retryable; `Retry-After` is respected when present; attempts are finite and cannot cross the lineage deadline.
4. Board-list technical failure yields the exact failed wrapper; catalog technical failure remains visible on its board; unexplained absence is not converted to a failure or invented explanation.
5. Prior-lineage data cannot enter a new construction when an upstream resource disappears or changes.
6. Outbound failures preserve their cause for spans/logs while diagnostics remain secret-free and metric labels remain low cardinality.

**Unit or integration validation**

- Integration-test against a controllable HTTP server that returns ordered multi-page catalogs, rate limiting with `Retry-After`, permanent failures, slow responses, and disconnects.
- Unit-test first-250 selection across page boundaries, failure classification, concurrency, cancellation, User-Agent generation, and absence semantics.

### US-004 — Complete lineages with bounded thread acquisition

**Goal**

Expand each selected catalog entry into a bounded, immutable textual thread resource without changing upstream order or hiding incomplete results.

**User value**

The Reader gets the first 250 posts exactly as observed, with oversized, failed, and deadline-expired threads still visible and understandable.

**Scope**

- Fetch the thread belonging to each of the first 250 catalog summaries through the shared outbound limiter and retry policy.
- Preserve every returned post object, original post HTML, media reference, and post order unchanged.
- Store zero through 250 posts as `present`; truncate more than 250 to the first 250 and mark the resource `oversize`.
- Mark terminal/exhausted failures and unfinished resources at the 30-minute lineage deadline as `failed` without a failure-detail payload.
- Stop scheduling or continuing unfinished acquisition after deadline/cancellation and propagate cancellation through workers.
- Record oversize detection and errors as meaningful logs and child spans; expose failed-resource counts without high-cardinality labels.

**Out of scope**

- Retrieval of posts beyond 250, on-demand fetches, media download/proxy/cache, Memcached publication, or retry queues/background repair.

**Dependencies**

- US-003.

**Likely related architectural decisions**

- None likely. Worker-pool mechanics are a local Go implementation detail under the locked global limits.

**Exact SEED traceability**

- `Full Requirements / Backend cache` and `/ Failure handling` (`docs/SEED.md:108-116`, `180-190`): 250-post cap, oversize marking, unchanged textual resources, failed resources, and deadline behavior.
- `High-Level Architecture / Snapshot contents` and `/ Upstream acquisition` (`docs/SEED.md:348-374`, `496-528`): required thread resources, unchanged HTML/media references, bounded acquisition, and no binary caching.
- `Operational Flows / Thread acquisition` and `/ Retry behavior` (`docs/SEED.md:753-811`): present/oversize behavior, terminal failures, retries, and deadline.
- `Failure Semantics / Upstream failures` (`docs/SEED.md:1724-1734`): transient retry and unfinished-resource failure.
- `Locked Decisions / Backend cache` and `/ Upstream` (`docs/SEED.md:2126-2134`, `2197-2205`): fixed caps, unchanged HTML, binary exclusion, global limiting, and time bounds.

**Objective acceptance criteria**

1. Every eligible catalog summary retains its original position and has a thread resource unless the upstream thread is genuinely unobserved/absent.
2. Responses of 0–250 posts produce `present` with the same ordered opaque posts; responses over 250 produce `oversize` with exactly the first 250.
3. No backend path retrieves or exposes post 251 or any media binary.
4. Terminal/exhausted requests and requests unfinished at the lineage deadline become exact failed wrappers; no work continues after cancellation/deadline.
5. A changed, missing, or newly oversized thread is evaluated only from the current observation, never copied from a prior lineage.
6. Oversize and failure telemetry is meaningful, cause-preserving, and free of raw post content or high-cardinality metric labels.

**Unit or integration validation**

- Integration-test 0, 250, and 251-post responses; transient/permanent errors; a deadline with queued and in-flight requests; and context cancellation.
- Unit-test truncation, order/opaque-value preservation, failed-resource counting, and absence distinction.

### US-005 — Publish one immutable lineage atomically through Memcached

**Goal**

Make a completed logical lineage available through an ephemeral Memcached namespace without exposing partial publication or sacrificing the prior active lineage on failure.

**User value**

The Reader sees either the last complete server snapshot or the next complete one, never a partially written mixture.

**Scope**

- Split a validated lineage into deterministic Memcached blocks below item limits, with lineage-scoped immutable keys, completion metadata, and one active-lineage pointer.
- Write and verify all required blocks and completion metadata before changing the active pointer.
- Preserve the old active pointer on construction validation, cache write, publication, or cancellation failure.
- After successful activation, evict the previous lineage immediately and assign all lineage keys a TTL of twice the configured synchronization interval as cleanup insurance.
- If post-activation eviction fails, keep the new lineage active, report the cleanup error, and rely on TTL rather than rolling the active pointer back.
- Treat Memcached as disposable serving state with no durable fallback.
- Trace cache operations and lineage validation/activation/eviction, update cache metrics, and log only lifecycle transitions, cleanup, and errors.
- Document key lifecycle and cache-loss recovery semantics without disclosing actual keys in telemetry.

**Out of scope**

- Durable storage, cache replication, distributed locking, multiple active lineages, historical browsing, HTTP snapshot serving, or fallback stores.

**Dependencies**

- US-002.
- US-004.

**Likely related architectural decisions**

- Proposed MADR 1, “Store backend lineages as ordered fixed-size serialized blocks.” Exact block byte size, key spelling, and cache-operation mechanics remain story-level details.

**Exact SEED traceability**

- `Full Requirements / Snapshot model` and `/ Failure handling` (`docs/SEED.md:98-107`, `180-190`): immutable independent lineages, atomic activation, and prior-lineage preservation.
- `High-Level Architecture / Backend` (`docs/SEED.md:306-342`): one active/building lineage, block-before-pointer order, immediate eviction, twice-interval TTL, and ephemeral cache behavior.
- `Operational Flows / Lineage construction and activation` (`docs/SEED.md:812-839`): all writes and completion metadata precede pointer switch; listed failures preserve active lineage.
- `Design Notes / Memcached as a serving cache` (`docs/SEED.md:1363-1373`): active pointer, immediate removal, and TTL as fallback only.
- `Detailed Observability / Tracing`, `/ Cache metrics`, and `/ Logging` (`docs/SEED.md:1481-1587`): cache/lifecycle spans, cache signals, and lifecycle-only logs.

**Objective acceptance criteria**

1. Before activation, no reader of the active pointer can resolve the building lineage; after activation, all required blocks and completion metadata are resolvable.
2. Any injected validation, write, metadata, pointer-publication, or cancellation failure leaves the previous pointer and its readable blocks intact.
3. A successful switch makes exactly one lineage active, attempts immediate deletion of previous keys, and leaves twice-interval TTLs on lineage keys for residual cleanup.
4. Failure to evict old inactive keys after a successful switch keeps the new lineage active, emits an error, and leaves TTL cleanup in place; it never rolls back to the old pointer.
5. Restart or complete Memcached loss is treated as empty ephemeral state; no file/database fallback or recovery copy exists.
6. Cache/lifecycle spans expose operation and outcome but not Memcached keys; metrics remain low cardinality and errors retain their causes.

**Unit or integration validation**

- Integration-test publication against Memcached, with injected failure after each write phase and concurrent readers observing the pointer.
- Unit-test deterministic blocking/reassembly, TTL calculation, pointer preservation, and eviction-key selection.

### US-006 — Serve the active lineage as one logical snapshot

**Goal**

Expose the active Memcached lineage through the backend's only snapshot route with unambiguous cache-loss semantics.

**User value**

The Reader can download one complete JSON snapshot, while an unavailable or expired server snapshot is reported clearly enough to preserve the browser's local copy.

**Scope**

- Serve internal `GET /snapshot` using the active pointer, completion metadata, and every required lineage block.
- Verify all required blocks before committing success, then stream/reassemble exactly one logical schema-version-1 JSON response without adding a public manifest, block, range, or resource endpoint.
- Return `410 Gone` if the pointer, completion metadata, or any required block is absent/incomplete; propagate other dependency/request failures meaningfully.
- Mark snapshot responses `Cache-Control: no-store` so neither intermediaries nor the Service Worker become an alternate textual serving cache.
- Do not apply Brotli compression in Go; leave normal HTTP content encoding to the VPS ingress.
- Create an HTTP root span with child cache-read and serialization spans, request metrics, and error-only logs; propagate request cancellation.
- Document `200`/`410` semantics and the internal route contract.

**Out of scope**

- `/api` prefix handling, Brotli ownership, client synchronization, resumable/fixed-block public transfer, individual board/thread endpoints, or upstream acquisition triggered by a request.

**Dependencies**

- US-001.
- US-005.

**Likely related architectural decisions**

- Proposed MADR 1, “Store backend lineages as ordered fixed-size serialized blocks,” which determines ordered reassembly and complete-block verification before a successful response. Buffer/stream mechanics remain local.

**Exact SEED traceability**

- `High-Level Architecture / HTTP routing` and `/ Snapshot transfer` (`docs/SEED.md:375-428`): internal route, logical response, ingress-owned Brotli, and excluded transfer mechanisms.
- `Operational Flows / Serving a snapshot` (`docs/SEED.md:840-867`): lookup sequence and `410 Gone` for any missing active component.
- `Failure Semantics / Cache failures` (`docs/SEED.md:1702-1713`): expired/unavailable snapshot meaning rather than resource `404`.
- `Operational Flows / Trace flow for inbound requests` (`docs/SEED.md:1081-1100`): root, pointer/metadata/block/serialization child spans, and error propagation.
- `Locked Decisions / Backend` (`docs/SEED.md:2169-2191`): browser/backend route mapping and ingress-only Brotli.

**Objective acceptance criteria**

1. After verifying every referenced block and before committing response headers, a complete active lineage returns `200` and one JSON document that passes both US-002 validators and preserves ordering/opaque values.
2. Missing pointer, metadata, or any referenced block returns `410` and never emits a partial successful document.
3. The backend exposes no manifest, block, range, per-board, per-thread, or acquisition-triggering route.
4. The response is not Brotli-compressed by the backend; request cancellation stops cache reads/serialization promptly.
5. Traces contain the required cache and serialization children; a failed request marks the relevant/root spans and logs the error without logging raw payloads or keys.
6. The snapshot response is explicitly non-cacheable by HTTP intermediaries/Service Worker and remains absent from Cache Storage.

**Unit or integration validation**

- Integration-test `200` reconstruction and each `410` missing-component case against Memcached.
- Unit-test method/route behavior, cancellation, serialization failure, telemetry attributes, and absence of application-level Brotli.

### US-007 — Build and activate lineages on the backend schedule

**Goal**

Join acquisition and publication into one instance-local, observable synchronization lifecycle that produces a new lineage from scratch on schedule.

**User value**

The Reader receives regularly refreshed snapshots even when some upstream resources fail, while the Operator can distinguish successful, degraded, and failed construction.

**Scope**

- Run one synchronization at a time, defaulting to hourly, after a stable instance-local startup jitter between 5 and 60 seconds.
- At construction start, create a new ULID and capture `observedAt` as UTC RFC 3339; enforce the 30-minute lineage deadline.
- Acquire boards, catalogs, and threads from scratch, validate the final contract, publish atomically, and evict the prior lineage only after success.
- Activate every successfully constructed/published lineage regardless of failed-resource count, including total 4chan outage represented by `boards.state = failed`.
- Preserve the active lineage for construction, validation, cache-write, publication, and cancellation failures.
- Apply a configured degradation tolerance only to observability: above tolerance, still activate, mark the synchronization root span error, log prominently, and retain lineage ID/failed/tolerated counts as trace/log attributes.
- Skip a scheduled tick when a synchronization is already active; do not overlap, queue, or cancel the current run, and record the skip as one meaningful scheduler event.
- Emit synchronization root spans, all required child operations, minimal lineage metrics, and meaningful start/completion/activation/eviction/acquisition-summary logs.
- Document cadence, jitter persistence scope, degradation threshold, cache-loss behavior, and next-scheduled-attempt recovery.

**Out of scope**

- Manual/client-triggered synchronization, overlapping runs, background repair/replay, distributed scheduling, guaranteed completion, or rejection based solely on degradation.

**Dependencies**

- US-003.
- US-004.
- US-005.

**Likely related architectural decisions**

- Proposed MADR 5, “Skip overlapping backend synchronization ticks.” Jitter derivation, degradation-tolerance defaults, and scheduler implementation mechanics remain local configuration/code choices.

**Exact SEED traceability**

- `Full Requirements / Snapshot model`, `/ Upstream acquisition`, and `/ Failure handling` (`docs/SEED.md:98-107`, `168-190`): independent lineages, cadence, jitter, deadline, activation, and preservation rules.
- `Operational Flows / Scheduled backend synchronization` (`docs/SEED.md:680-706`): ULID, deadline, acquisition order, publication, activation, eviction, and one-run behavior.
- `Operational Flows / Degraded lineage completion` (`docs/SEED.md:868-895`): observability-only tolerance and activation despite degradation.
- `Operational Flows / Trace flow for scheduled synchronization` and `/ Telemetry export` (`docs/SEED.md:1101-1153`): trace tree, lineage attribute, lifecycle signals, and excluded high-cardinality metrics.
- `Detailed Observability / Lineages`, `/ Logging`, `/ Error handling`, and `/ Sampling` (`docs/SEED.md:1551-1615`): duration/outcome/age metrics, meaningful logs, degraded error trace, and collector sampling intent.
- `Locked Decisions / Synchronization` and `/ Failure semantics` (`docs/SEED.md:2160-2168`, `2218-2228`): stable jitter, full replacement, degraded activation, total outage, and current-lineage preservation.

**Objective acceptance criteria**

1. The default schedule is hourly, its initial 5–60-second offset is stable for one backend instance, and a tick during an active run is skipped without overlap, queueing, or cancellation; the next ordinary tick remains the next opportunity.
2. Each run uses a new valid ULID and one start-time UTC `observedAt`, has a hard 30-minute deadline, and cannot consume prior-lineage resources.
3. Successful construction/publication activates the new lineage and evicts the old; all listed non-resource failures preserve the old active pointer.
4. Upstream resource failures, including total board-list failure, can produce and activate a contract-valid degraded lineage.
5. Crossing the configured tolerance changes telemetry only: activation still occurs, the root span becomes error, a prominent structured log is emitted, and the full trace is eligible for failed-trace retention.
6. Lineage metrics are limited to duration, successful/degraded outcome, failed count, and active age; identifiers do not become metric labels.

**Unit or integration validation**

- Unit-test stable jitter range/reuse, overlap-tick skipping and its event, ULID/time assignment, threshold classification, cancellation, and next-interval behavior with a fake clock.
- Integration-test complete, degraded, total-outage, deadline, publication-failure, and cache-loss runs through the acquisition and Memcached boundaries, asserting pointer and telemetry outcomes.

### US-008 — Install and reopen the application shell offline

**Goal**

Provide the Reader with a Chrome-for-Android-targeted Preact PWA shell that can be installed and reopened without a network connection.

**User value**

The Reader can launch 4Visor like an app and reach its local startup experience during server or network outages.

**Scope**

- Build the shell with Preact, TypeScript, Tailwind CSS, native ES modules, and Vite for Chrome for Android 150+.
- Supply a Web App Manifest and Service Worker using browser APIs directly.
- Cache only versioned application-shell/static assets in Cache Storage; never snapshot JSON or media.
- Serve the cached shell offline and provide a deterministic shell-cache update/removal policy.
- Keep Preact as the only framework and avoid a state-management framework, client-side router, SSR, or multi-page architecture.
- Document supported browser, installation/offline-shell behavior, and cache boundaries.

**Out of scope**

- IndexedDB snapshot data, snapshot synchronization, content rendering, media caching, cross-browser compatibility, or an offline media promise.

**Dependencies**

- None.

**Likely related architectural decisions**

- None likely. Cache Storage boundaries and browser/framework choices are locked; shell precache/update mechanics and asset versioning are local implementation details.

**Exact SEED traceability**

- `Vision`, `Axioms`, and `Full Requirements / Product` (`docs/SEED.md:3-29`, `40-79`, `86-97`): read-only anonymous PWA, browser serving layer, Preact-only lifecycle, and no identity/personalization.
- `Full Requirements / Local storage` (`docs/SEED.md:128-139`): Cache Storage holds shell/static assets only.
- `Technology Stack / Frontend` and `/ Browser platform` (`docs/SEED.md:1779-1791`, `1833-1841`): selected frontend tools and browser APIs.
- `Technology Rationale / Preact`, `/ Tailwind CSS`, `/ Vite`, and `/ Service Worker` (`docs/SEED.md:1938-1975`, `2002-2013`): narrow framework/tooling and offline shell rationale.
- `Locked Decisions / Frontend` (`docs/SEED.md:2135-2151`): exact frontend stack, Chrome target, Service Worker boundary, and router exclusions.

**Objective acceptance criteria**

1. The production build produces a valid installable manifest and a Service Worker-controlled shell for Chrome for Android 150+.
2. After one successful shell load, an integration test can request the shell/static assets with network fetches failing and receive them from Cache Storage.
3. Cache Storage contains no snapshot response, IndexedDB data, or explicitly cached media.
4. Activating a new shell cache removes obsolete application-shell caches without clearing IndexedDB or browser-managed HTTP media cache.
5. The source/runtime dependency graph contains Preact but no additional UI/state/router/SSR framework.

**Unit or integration validation**

- Unit-test shell-cache allowlisting and version cleanup.
- Integration-test Service Worker install/activate/fetch behavior with browser API test doubles; do not create an end-to-end browser test.

### US-009 — Start from, fail on, or reset local snapshot storage

**Goal**

Make IndexedDB the mandatory and exclusive home for local snapshots, with immediate startup from the active lineage and a complete user-controlled reset.

**User value**

The Reader can open the last snapshot immediately, gets a clear explanation when required storage is unusable, and can recover from corruption by resetting all 4Visor-local data.

**Scope**

- Open the application IndexedDB database at startup and load the active lineage without waiting for the backend.
- Represent one active lineage plus, only during synchronization, one incoming lineage and the installation jitter seed.
- Show a clear mandatory-storage error for unavailable/corrupt IndexedDB, with no memory-only or online-only fallback.
- Show a clear empty state when no active lineage exists.
- Provide a confirmed “Reset local data” action that deletes application IndexedDB data, incoming data, jitter seed, and application shell caches, then reloads.
- Ensure reset performs no server-side call and document its local-only effect.

**Out of scope**

- Downloading/activating a replacement, periodic refresh, browser HTTP cache deletion, server cache reset, or storage migration across snapshot schema versions.

**Dependencies**

- US-002.
- US-008.

**Likely related architectural decisions**

- Proposed MADR 2, “Store each browser lineage as one opaque IndexedDB record.” Corruption detection and database naming/version mechanics remain local; version-1 migration stays excluded.

**Exact SEED traceability**

- `Full Requirements / Local storage` (`docs/SEED.md:128-139`): IndexedDB-only snapshots, mandatory failure, reset, and quota semantics boundary.
- `High-Level Architecture / Client architecture` (`docs/SEED.md:269-305`): one active plus temporary incoming lineage and immediate local rendering.
- `Operational Flows / Client startup` (`docs/SEED.md:599-623`): mandatory open, immediate active render, empty state, and no fallback.
- `Operational Flows / Local reset` (`docs/SEED.md:1005-1026`): confirmation, database/cache/seed removal, reload, and local-only effect.
- `Failure Semantics / Client failures` (`docs/SEED.md:1655-1665`): unavailable/corrupt IndexedDB and recovery behavior.

**Objective acceptance criteria**

1. Startup with a valid active lineage reads it from IndexedDB and makes it available to the UI before any backend request is required.
2. Startup with no active lineage shows the explicit empty state; unavailable or corrupt IndexedDB shows a clear blocking storage error and does not use memory/online fallback.
3. The storage model can hold no more than one active and one incoming lineage, and snapshot payloads never enter Cache Storage.
4. Confirmed reset removes active/incoming lineages, jitter seed, and all 4Visor application-shell caches, performs no server request, and reloads; cancellation changes nothing.
5. Reset documentation tells the Reader that cached snapshot continuity and the stable local jitter will be lost.

**Unit or integration validation**

- Integration-test startup with valid, empty, unavailable, and corrupt IndexedDB using a browser-storage test double.
- Integration-test confirmed/cancelled reset across IndexedDB and Cache Storage and assert that no fetch occurs.

### US-010 — Replace the local lineage only after complete synchronization

**Goal**

Download, validate, stage, and atomically activate one backend-authoritative snapshot while preserving the Reader's current lineage on every failure.

**User value**

The Reader can continue reading a complete snapshot during refresh and after network, backend, schema, or storage failures.

**Scope**

- Fetch one logical snapshot from browser route `GET /api/snapshot` only when a synchronization is due.
- Parse and validate the entire payload against exact schema version 1, stage it in temporary IndexedDB storage, then atomically switch the active pointer.
- Keep the old active lineage visible until commit succeeds; after success delete the previous lineage and render the replacement.
- Accept the backend's lineage without timestamp/ULID comparison, merge, reconciliation, or partial activation.
- On network/HTTP/`410`, parse/schema, quota, IndexedDB, or activation failure, keep the current lineage, leave incoming data inactive/cleanable, show a clear classified error, and wait for the next scheduled attempt.
- Propagate abort/cancellation and avoid requests for individual missing resources.
- Document incompatible-deployment, quota, server-unavailable, and first-sync behavior.

**Out of scope**

- Periodic timer/jitter policy, resumable downloads, multiple public blocks, differential sync, migration/adapters, background retry, or resource-specific backend calls.

**Dependencies**

- US-002.
- US-006.
- US-009.

**Likely related architectural decisions**

- Proposed MADR 2, “Store each browser lineage as one opaque IndexedDB record,” for staging/promotion/removal.
- Proposed MADR 3, “Enforce snapshot version 1 with boundary validators and shared fixtures,” for activation validation. One-response transfer is already the locked initial interpretation, not another MADR.

**Exact SEED traceability**

- `Full Requirements / Client synchronization` and `/ Local storage` (`docs/SEED.md:117-139`): complete-before-activation, prior preservation, one active lineage, IndexedDB, and quota handling.
- `High-Level Architecture / Client architecture` and `/ Snapshot transfer` (`docs/SEED.md:269-305`, `395-428`): stage, validate, atomic swap, one logical response, and browser decompression.
- `Operational Flows / Client synchronization` (`docs/SEED.md:624-658`): exact success/failure path and backend authority.
- `Failure Semantics / Client failures` and `/ Synchronization failures` (`docs/SEED.md:1655-1681`): network/backend/schema/storage outcomes and no partial activation.
- `Locked Decisions / Snapshot model` and `/ Synchronization` (`docs/SEED.md:2115-2125`, `2160-2168`): no reconciliation/compatibility, complete replacement, and prior retention.

**Objective acceptance criteria**

1. During download, validation, and staging, reads/rendering continue to resolve the old active lineage.
2. A valid complete response is staged, committed with one atomic active-pointer change, followed by previous-lineage deletion; only one active lineage remains.
3. Network failure, non-success HTTP including `410`, invalid JSON/schema, incompatible version, quota/storage failure, and cancellation leave the old active lineage unchanged and produce distinct clear user-facing errors.
4. A valid backend lineage is activated even if its identifier/time is older than the local one; no merge, comparison, or partial progressive rendering occurs.
5. The client makes no textual-content request other than the complete snapshot request and schedules no immediate automatic retry.

**Unit or integration validation**

- Integration-test every stage with injected failure and concurrent active reads, asserting pointer/data invariants and user error classification.
- Unit-test response classification, backend-authority behavior, abort propagation, and old/incoming cleanup decisions.

### US-011 — Refresh with stable installation-local jitter

**Goal**

Schedule browser synchronization approximately hourly using a stable, private installation-local offset.

**User value**

The Reader receives unattended refreshes without synchronized client bursts or identity/fingerprinting data leaving the browser.

**Scope**

- On first activation, generate a random local seed without device/browser fingerprinting inputs, persist it in IndexedDB, and derive a stable 5–60-second jitter.
- Reuse the same offset until local reset and never transmit the seed.
- Trigger one complete synchronization at the documented approximately-hourly cadence and wait until the next cadence after any success or failure.
- Prevent overlapping browser synchronizations and continue rendering the active lineage while waiting/running.
- Document the concrete first-attempt and subsequent timer arithmetic while preserving the locked approximately-hourly stable-offset behavior.

**Out of scope**

- User-configurable preferences, server-side client state, notifications, immediate retry loops, background sync APIs not required by the SEED, or client-triggered backend acquisition.

**Dependencies**

- US-009.
- US-010.

**Likely related architectural decisions**

- None likely. Exact timer arithmetic is a local implementation detail; the stable, private, approximately-hourly behavior is locked.

**Exact SEED traceability**

- `Full Requirements / Client synchronization` (`docs/SEED.md:117-127`): approximately hourly refresh and stable installation-local jitter.
- `Operational Flows / Client synchronization` and `/ First installation jitter` (`docs/SEED.md:624-679`): next-scheduled retry, random local seed, privacy, reset lifecycle, and stable derivation.
- `Axioms` (`docs/SEED.md:40-79`): anonymous operation and only synchronization/offline local state.
- `Locked Decisions / Product` and `/ Synchronization` (`docs/SEED.md:2101-2114`, `2160-2168`): no identity/preferences and stable complete refresh.

**Objective acceptance criteria**

1. A fresh installation stores a randomly generated seed and deterministically derives a jitter in the inclusive 5–60-second range; reloads reuse the same value.
2. The seed uses no fingerprinting/device attributes, is absent from snapshot requests and telemetry, and is removed by US-009 reset.
3. The selected documented cadence produces approximately one attempt per hour, never overlaps attempts, and does not accumulate a new random offset each cycle.
4. Success and all failure classes wait for the next scheduled interval rather than starting an immediate automatic retry.
5. Existing local content remains readable throughout timer wait and synchronization.

**Unit or integration validation**

- Unit-test seed derivation range/stability/privacy, reset regeneration, cadence, no-overlap, and post-failure scheduling with a fake clock and deterministic randomness.
- Integration-test the scheduler against the US-010 synchronization boundary without making real network calls.

### US-012 — Browse ordered boards and compact catalogs from the local lineage

**Goal**

Render the active lineage's boards and catalogs as a responsive, content-focused local browsing experience.

**User value**

The Reader can browse the observed board/catalog hierarchy quickly on mobile while always knowing which snapshot is being viewed and how stale it is.

**Scope**

- Render boards and catalog pages/threads exclusively from the active IndexedDB lineage, preserving all upstream orders and page boundaries.
- Present board catalogs as responsive mobile-first compact rows.
- Keep lineage ULID and calculated snapshot age visible in all catalog/board states.
- Keep failed catalogs/resources in their original position with clear degraded presentation; show “Not available in this snapshot” for absent local resources.
- Provide board/thread selection with component state and native browser history only; do not add a client-side routing framework or fabricate canonical 4Visor content URLs.
- Expose original canonical 4chan destinations where the snapshot supplies them.
- Use semantic, keyboard-operable controls and readable status text for basic accessibility.

**Out of scope**

- Thread post-body rendering, HTML sanitization, media loading, sorting/filtering/search, read state, bookmarks, recommendations, or resource fetch-on-selection.

**Dependencies**

- US-008.
- US-009.
- US-010.

**Likely related architectural decisions**

- None likely. View/history state and reply-selection mechanics are local frontend details under locked no-router and upstream-order constraints.

**Exact SEED traceability**

- `Full Requirements / Product` and `/ User interface` (`docs/SEED.md:86-97`, `159-167`): read-only behavior, canonical URLs, exact ordering, mobile-first compact catalogs, visible lineage/age, and degraded resources.
- `Operational Flows / Client rendering` and `/ Missing local resource` (`docs/SEED.md:896-944`): local-only rendering, no reorder/filter/fetch, degradation, and absent-resource message.
- `Design Notes / Client-first design`, `/ Upstream fidelity`, and `/ Honest degradation` (`docs/SEED.md:1323-1333`, `1374-1383`, `1394-1405`): IndexedDB serving, exact observation, and visible failures.
- `Locked Decisions / Product` and `/ Frontend` (`docs/SEED.md:2101-2114`, `2135-2151`): exclusions, Chrome target, responsive compact rows, and no router.

**Objective acceptance criteria**

1. Given a fixture lineage, rendered board, page, and thread-summary order exactly matches the source arrays, including failed entries in place.
2. Catalogs use compact responsive rows at mobile and wider viewport breakpoints supported by the chosen Tailwind design.
3. Lineage ID and age remain visible in board/catalog, empty, failed, and absent-resource views.
4. Selecting an absent item shows the required message and performs no backend/upstream fetch; failed items have a distinct visible degraded state.
5. No search, filter, ranking, recommendation, bookmark, or read-state control exists, and navigation uses no client-side router dependency.
6. Interactive controls are semantic, keyboard operable, and expose degraded/empty status in text rather than color alone.

**Unit or integration validation**

- Use Vitest component integration tests for order, page boundaries, responsive classes/layout states, visible lineage metadata, degraded/absent states, and zero fetches on selection.
- Unit-test snapshot-age formatting with a fake clock.

### US-013 — Render upstream post markup safely with canonical links

**Goal**

Convert unchanged upstream HTML into safe Preact-renderable content while retaining supported meaning and making unsupported markup visible as text.

**User value**

The Reader can read formatted posts and follow legitimate links without upstream HTML gaining unsafe access to the main document.

**Scope**

- Parse upstream post HTML in an isolated DOM representation and apply a strict documented element/attribute/protocol allowlist before rendering.
- Render supported safe markup; convert unsupported elements/attributes to visible plain text rather than silently dropping their textual content.
- Keep external links clickable at their original safe destination.
- Resolve quote links to canonical 4chan thread/post URLs, never internal PWA navigation.
- Ensure no code path injects unsanitized upstream HTML into the main document.
- Document the allowlist, URL/protocol policy, unsupported fallback, and security invariants.

**Out of scope**

- Rewriting upstream HTML in the backend/cache, inline navigation, link previews, content moderation, media proxying, or sanitizing trusted application templates.

**Dependencies**

- US-002.
- US-008.

**Likely related architectural decisions**

- Proposed MADR 4, “Sanitize upstream HTML with a proven browser-side allowlist sanitizer,” including the frontend trust boundary, explicit allowlist, safe-link policy, and visible-text fallback.

**Exact SEED traceability**

- `Full Requirements / Rendering` (`docs/SEED.md:140-148`): unchanged backend HTML, frontend sanitization, text fallback, external links, canonical quote links, and no unsafe injection.
- `Operational Flows / Post markup rendering` (`docs/SEED.md:945-965`): isolated parse, strict allowlist, text fallback, and original link destinations.
- `Design Notes / Upstream fidelity` (`docs/SEED.md:1374-1383`): original HTML is preserved by the backend.
- `Locked Decisions / Backend cache` and `/ Rendering` (`docs/SEED.md:2126-2134`, `2152-2159`): unchanged storage and mandatory safe frontend rendering.

**Objective acceptance criteria**

1. Representative supported 4chan markup retains intended text/formatting after sanitization.
2. Scripts, event handlers, unsafe URL protocols, executable/embedded content, style-based escape vectors, and unknown dangerous attributes cannot enter the rendered main document.
3. Unsupported markup's user-visible text remains visible as plain text rather than disappearing or executing.
4. Safe external links remain clickable at the same destination; quote links resolve to canonical 4chan URLs and do not invoke internal routing.
5. Rendering APIs receive only sanitizer output, with no alternate raw-HTML path.

**Unit or integration validation**

- Table-driven Vitest unit tests cover supported markup, nested unsupported markup, malformed HTML, XSS payload classes, protocol handling, external URLs, and quote URLs.
- Component integration-test that only sanitized nodes reach the post renderer.

### US-014 — Read nested, collapsible threads with honest degradation

**Goal**

Render a selected thread's ordered posts in a compact nested and collapsible local view, including failed, absent, and oversized outcomes.

**User value**

The Reader can follow a conversation comfortably while seeing exactly where the frozen snapshot is incomplete or truncated.

**Scope**

- Render thread posts from the active local lineage in upstream order using the sanitized markup boundary.
- Derive and display visual reply nesting from available upstream reply/quote relationships without reordering posts.
- Make posts collapsible with accessible native/Preact controls and no persisted read/preference state.
- Keep lineage ID and age visible in the thread view.
- Render `failed` threads in place, `oversize` threads with the first 250 posts plus a clear truncation notice, and absent threads as “Not available in this snapshot.”
- Offer canonical 4chan thread/post links for unavailable content or quotes; never fetch missing/additional posts.

**Out of scope**

- Posts after the first 250, inline reply navigation as application routing, posting/replying/moderation, read tracking, saved collapse state, search, filtering, or backend reconciliation.

**Dependencies**

- US-012.
- US-013.

**Likely related architectural decisions**

- None likely. Reply nesting is presentation-only and must retain post sequence; the exact parent/indentation heuristic is a local UI choice.

**Exact SEED traceability**

- `Full Requirements / User interface` (`docs/SEED.md:159-167`): responsive layout, nested replies, collapsible posts, visible lineage/age, and degraded resources.
- `Operational Flows / Client rendering` and `/ Missing local resource` (`docs/SEED.md:896-944`): local-only ordered content, visible failed/oversize resources, and absent message.
- `Failure Semantics / Failure matrix` (`docs/SEED.md:1735-1749`): oversize first 250 and missing-resource outcomes.
- `Locked Decisions / Frontend` and `/ Rendering` (`docs/SEED.md:2135-2159`): nesting, collapse, no router, sanitization, canonical links, and degradation.

**Objective acceptance criteria**

1. Posts appear in the exact snapshot order; visual nesting changes presentation only and cannot reorder/filter content.
2. Every post can be expanded/collapsed with a semantic keyboard-operable control, and collapse state is not persisted as preference/read state.
3. `oversize` renders exactly the stored first 250 posts and an explicit truncation message; `failed` and absent states remain visibly distinct and in context.
4. Lineage ID/age remain visible and every post body is rendered through US-013 sanitation.
5. Selecting missing/additional content makes zero backend requests and offers only the applicable canonical external 4chan destination.

**Unit or integration validation**

- Vitest component integration tests cover order-preserving nesting, collapse interaction, sanitizer use, lineage metadata, present/failed/oversize/absent states, canonical links, accessibility roles, and zero resource fetches.

### US-015 — Load media directly and only with the required user intent

**Goal**

Display upstream media references without involving backend storage and without automatically fetching full-resolution content.

**User value**

The Reader sees useful thumbnails online, controls expensive/full media loads, and gets a stable retryable placeholder when media is offline or unavailable.

**Scope**

- Request thumbnails directly from their original 4chan URLs while online.
- Load full-size image/video/audio/file media only after explicit user interaction, using native browser behavior where appropriate.
- Keep spoiler media hidden until explicit reveal.
- On offline/error, display one fixed textual/visual placeholder and permit only user-initiated retry without an app-imposed attempt limit.
- Perform no explicit Service Worker, Cache Storage, IndexedDB, backend, proxy, transform, or application cache operation for media; ordinary browser HTTP caching remains outside the model.
- Ensure media failure never changes textual snapshot availability.

**Out of scope**

- Offline media persistence, preload, automatic retry, proxying, transcoding, optimization, thumbnails generated by 4Visor, or backend binary acquisition.

**Dependencies**

- US-014.

**Likely related architectural decisions**

- None likely; direct original URLs, intent gating, spoiler behavior, and no explicit cache are locked. Native elements should cover the implementation.

**Exact SEED traceability**

- `Full Requirements / Media` (`docs/SEED.md:149-158`): automatic online thumbnails, explicit full media, browser cache only, placeholder/manual retry, and spoilers.
- `High-Level Architecture / Media path` (`docs/SEED.md:529-547`): backend bypass, direct request, ordinary cache, and manual retry.
- `Operational Flows / Thumbnail loading` and `/ Full media loading` (`docs/SEED.md:966-1004`): online/error paths, explicit action, native behavior, and no proxy/transform/persist.
- `Failure Semantics / Media failures` (`docs/SEED.md:1714-1723`): independent textual availability and no automatic retry.
- `Out of Scope / Media` (`docs/SEED.md:2314-2322`): locked caching/persistence/retry/transcoding exclusions.

**Objective acceptance criteria**

1. Online posts with media request only their original thumbnail automatically; no full-resolution URL is requested before explicit open/reveal action.
2. Spoiler media has no visible content and does not trigger its full-media request until explicitly revealed.
3. Offline or failed media displays the fixed placeholder without disturbing post text; only a user action retries.
4. Media requests go directly to original 4chan URLs and never to `/api`, IndexedDB, Service Worker-managed caches, or an application proxy.
5. No automatic retry timer or application-managed media cache exists; native browser HTTP caching is left untouched.

**Unit or integration validation**

- Vitest component integration tests use mocked browser/network signals to assert thumbnail timing, full-media intent gating, spoiler reveal, failure placeholder, manual-only retries, direct URLs, and unchanged textual rendering.

### US-016 — Package the backend and PWA as hardened first-party images

**Goal**

Produce the two project-owned Linux-amd64 runtime images required by the deployment model: Go backend and frontend Caddy/static assets.

**User value**

The Operator can build small, deterministic runtime artifacts that serve 4Visor without shells, root privileges, or writable root filesystems.

**Scope**

- Build the Go backend as a Linux amd64 static runtime artifact in a project-owned distroless image that runs as a non-root user and supports a read-only root filesystem.
- Build the production PWA and serve its assets with Caddy in a separate project-owned distroless/rootless/read-only-compatible image.
- Configure frontend Caddy only for static shell/assets; it neither proxies `/api` nor owns Brotli compression.
- Keep runtime writes out of project-owned root filesystems and document image build, architecture, user, filesystem, and runtime configuration assumptions.
- Do not rebuild or impose project-owned hardening on Memcached, the OpenTelemetry Collector, or other third-party images.

**Out of scope**

- Compose topology/host ports, edge routing, TLS, ingress configuration, multi-architecture images, registry publication, signing/SBOM systems, or enterprise container hardening.

**Dependencies**

- US-006.
- US-008.

**Likely related architectural decisions**

- None likely. The first-party hardening/runtime topology is locked; exact maintained bases and Dockerfile mechanics are local implementation choices.

**Exact SEED traceability**

- `Full Requirements / Deployment` and `/ Platform and testing` (`docs/SEED.md:191-217`): separate services, frontend Caddy/backend direct server, first-party hardening, and Linux amd64.
- `Deployment View / Container model` (`docs/SEED.md:1202-1229`): project versus third-party images and native configuration boundaries.
- `Technology Stack / Infrastructure` and `/ Operating systems` (`docs/SEED.md:1799-1807`, `1842-1851`): Docker, Caddy, distroless/rootless/read-only, and amd64.
- `Technology Rationale / First-party container hardening` (`docs/SEED.md:2027-2043`): reduced attack surface and third-party exemption.
- `Locked Decisions / Backend` (`docs/SEED.md:2169-2191`): separate containers, ingress compression ownership, hardening, and target architecture.

**Objective acceptance criteria**

1. Both first-party images build for Linux amd64 from repository sources and contain only their required runtime artifact/configuration.
2. Image metadata/runtime configuration uses a non-root user, and each service can perform its normal work with a read-only root filesystem and no shell-dependent startup.
3. The backend image exposes the Go server directly; the frontend image serves built assets through Caddy and contains no API proxy/Brotli responsibility.
4. Third-party images are referenced upstream rather than rebuilt merely for distroless/rootless/read-only controls.
5. Operator documentation states supported architecture and the required runtime mounts/environment without claiming multi-architecture or enterprise hardening.

**Unit or integration validation**

- Integration validation builds each image, inspects configured user/architecture/entrypoint, and runs the contained process under a read-only filesystem in isolation; this is artifact integration validation, not a deployment/smoke test.
- Run the backend and frontend build/unit checks before image assembly.

### US-017 — Deploy one loopback edge with private internal services

**Goal**

Compose the edge, frontend, backend, Memcached, and Collector so the VPS ingress reaches exactly one loopback-bound service and browser API paths map to the correct internal routes.

**User value**

The Operator can deploy and upgrade the personal instance with a small topology that does not expose backend dependencies to the Internet.

**Scope**

- Define Docker Compose services for dedicated edge Caddy, frontend Caddy, Go backend, one Memcached, and one OpenTelemetry Collector.
- Bind only edge Caddy to an explicit `127.0.0.1` port in the mandated 65100–65199 project range; give no host port to frontend, backend, Memcached, or Collector.
- Route `/api/*` first, strip `/api`, and proxy to backend so `/api/snapshot` and `/api/health` become `/snapshot` and `/health`; route all other requests to frontend Caddy.
- Keep TLS termination and Brotli response compression at the existing VPS ingress only; both Caddy services forward uncompressed internal HTTP.
- Limit Memcached reachability to the backend-facing internal network and use native Caddy/Memcached/Collector configuration rather than `FOURVISOR_` variables for third-party services.
- Configure Compose health checks around backend responsiveness, Memcached availability, and 4chan DNS through the shallow backend health contract, without inventing readiness orchestration.
- Document loopback ingress wiring, configuration, start/stop/upgrade, accepted single-service outages, and recovery (including waiting for scheduled rebuild after Memcached loss).

**Out of scope**

- Public `0.0.0.0`/IPv6 host binds, Docker firewall changes, TLS certificates inside Compose, Brotli in either Caddy/backend, Kubernetes, replicas, autoscaling, service mesh, complex readiness/liveness, or automated deployment tests.

**Dependencies**

- US-001.
- US-006.
- US-016.

**Likely related architectural decisions**

- None likely. The five-service topology, edge routing, and exposure constraints are locked; network names and the concrete compliant loopback port are local deployment details.

**Exact SEED traceability**

- `Full Requirements / Deployment` (`docs/SEED.md:191-210`): Compose, loopback-only edge, prefix stripping, separate services, native configuration, health, TLS, and hardening boundaries.
- `High-Level Architecture` and `/ HTTP routing` (`docs/SEED.md:230-268`, `375-394`): traffic topology and exact browser/backend route mapping.
- `Deployment View / Deployment philosophy`, `/ Container model`, `/ Health model`, `/ Traffic`, `/ Failure model`, and `/ Security` (`docs/SEED.md:1175-1291`): one exposed edge, internal services, health, direct media, accepted outages, and private Memcached.
- `Technology Stack / Networking` (`docs/SEED.md:1808-1817`): HTTPS ingress, loopback Caddy, prefix stripping, internal network, and upstream HTTP.
- `Locked Decisions / Backend` and `/ Out of scope` (`docs/SEED.md:2169-2191`, `2229-2245`): deployment/routing/hardening and distributed/platform exclusions.
- `AGENTS.md / Guidelines for Workers`: service ports must be 65100–65199 and Docker exposure must bind directly to `127.0.0.1`.

**Objective acceptance criteria**

1. Rendered Compose configuration has exactly one host-published port, explicitly bound to `127.0.0.1` in 65100–65199, belonging to edge Caddy; no other service has host exposure.
2. Edge Caddy precedence strips `/api` and maps the two browser routes to backend routes while every non-API request goes to frontend Caddy.
3. Backend alone can reach Memcached on an internal network; browser/host cannot address Memcached, backend, frontend, or Collector directly through published ports.
4. No Compose service terminates TLS or applies Brotli; documentation identifies VPS ingress as owner of both and uses loopback HTTP to edge.
5. Health configuration uses the existing shallow `/health` contract and adds no readiness endpoint or complex orchestration.
6. The documented upgrade/recovery path accepts temporary outages and never proposes replicas, persistent cache recovery, client-triggered acquisition, or firewall changes.

**Unit or integration validation**

- Integration-validate rendered Compose and Caddy configuration for service references, route order/prefix stripping, network membership, health command, port range, loopback address, and absence of other published ports/TLS/Brotli directives.
- Do not add a deployment or smoke test.

### US-018 — Retain failed traces and a sample of successful operation

**Goal**

Complete the trace-first operational path from the Go backend through the internal Collector to configurable metric, log, and trace destinations.

**User value**

The Operator can explain a failed request or degraded lineage from one trace while keeping telemetry volume and personal-project operations small.

**Scope**

- Configure the internal OpenTelemetry Collector to receive backend OTLP telemetry, export minimal metrics/logs, and perform tail-based trace sampling.
- Retain every trace containing an error and approximately 10% of fully successful traces; perform no application-side sampling.
- Verify inbound request and scheduled synchronization root traces contain the required outbound HTTP, Memcached, validation/serialization, construction, activation, and eviction children where applicable.
- Export only the enumerated low-cardinality HTTP/cache/lineage metrics and meaningful lifecycle/error logs.
- Keep Caddy and third-party container stdout outside the Go OpenTelemetry contract; Collector/exporter failure must not fail application operations.
- Document signal names/attributes, how to locate a degraded sync by lineage ULID, exporter configuration, expected single-node gaps, and a concise operator troubleshooting flow.

**Out of scope**

- A second observability stack, application-side sampling, local telemetry buffering, verbose request logs, high-cardinality metrics, audit/business/user analytics, session replay, or enterprise alerting/SLO machinery.

**Dependencies**

- US-001.
- US-006.
- US-007.
- US-017.

**Likely related architectural decisions**

- None likely. The telemetry topology and sampling behavior are locked; concrete exporter destinations and Collector syntax are operator configuration/implementation details.

**Exact SEED traceability**

- `Full Requirements / Observability` (`docs/SEED.md:218-229`): Go OpenTelemetry boundary, sparse metrics/logs, root/child traces, and sampling outcomes.
- `High-Level Architecture / Observability path` (`docs/SEED.md:548-596`): root/child topology and Collector tail sampling.
- `Operational Flows / Trace flow for inbound requests`, `/ Trace flow for scheduled synchronization`, and `/ Telemetry export` (`docs/SEED.md:1081-1153`): required spans, events, exported logs, and excluded labels.
- `Deployment View / Observability` (`docs/SEED.md:1292-1302`): backend-to-Collector path and third-party stdout boundary.
- `Detailed Observability` (`docs/SEED.md:1450-1629`): exact philosophy, signal set, error propagation, sampling, no buffering, and diagnostic questions.
- `Locked Decisions / Observability` (`docs/SEED.md:2206-2217`): trace-first, minimal telemetry, tail sampling, and retention percentages.
- `Out of Scope / Observability` (`docs/SEED.md:2353-2361`): verbose/high-cardinality/audit/analytics exclusions.

**Objective acceptance criteria**

1. Collector configuration receives OTLP from the backend and tail-samples 100% of traces containing an error plus approximately 10% of fully successful traces; backend SDK sampling is always-on/deferred to Collector.
2. Representative successful and failed `/health`, `/snapshot`, and scheduled-sync traces have one root and the applicable required child operations with errors propagated to relevant parents.
3. Exported metrics are limited to HTTP request/latency, cache operation/hit/miss/error/latency, synchronization duration/outcome, failed-resource count, and active-lineage age; forbidden identifiers/raw values are absent from labels.
4. Exported logs contain meaningful lineage lifecycle/acquisition summaries and all errors, but no routine successful request, successful cache GET, successful outbound request, or individual cache-hit chatter.
5. Collector/exporter unavailability leaves health, snapshot serving, and synchronization behavior unchanged apart from lost telemetry.
6. Operator documentation can locate an excessive-degradation trace from lineage ID and explains expected personal-grade telemetry gaps without promising audit/SLO capabilities.

**Unit or integration validation**

- Integration-test backend telemetry through an in-process or test Collector pipeline with synthetic successful/failed traces, asserting child structure, filtering, metric label sets, and failure non-interference.
- Validate Collector configuration and tail-sampling policy deterministically; do not perform deployment or external-backend tests.

## Dependency-order audit

| Order | Story | Dependencies | Highest dependency order | Result |
| ---: | --- | --- | ---: | --- |
| 1 | US-001 | None | — | Valid |
| 2 | US-002 | None | — | Valid |
| 3 | US-003 | US-001, US-002 | 2 | Valid |
| 4 | US-004 | US-003 | 3 | Valid |
| 5 | US-005 | US-002, US-004 | 4 | Valid |
| 6 | US-006 | US-001, US-005 | 5 | Valid |
| 7 | US-007 | US-003, US-004, US-005 | 5 | Valid |
| 8 | US-008 | None | — | Valid |
| 9 | US-009 | US-002, US-008 | 8 | Valid |
| 10 | US-010 | US-002, US-006, US-009 | 9 | Valid |
| 11 | US-011 | US-009, US-010 | 10 | Valid |
| 12 | US-012 | US-008, US-009, US-010 | 10 | Valid |
| 13 | US-013 | US-002, US-008 | 8 | Valid |
| 14 | US-014 | US-012, US-013 | 13 | Valid |
| 15 | US-015 | US-014 | 14 | Valid |
| 16 | US-016 | US-006, US-008 | 8 | Valid |
| 17 | US-017 | US-001, US-006, US-016 | 16 | Valid |
| 18 | US-018 | US-001, US-006, US-007, US-017 | 17 | Valid |

No story depends on a later story.

## Coverage audit by SEED concern

| SEED concern | Covering stories |
| --- | --- |
| Product axioms, anonymity, read-only behavior, fidelity, and locked exclusions | US-002, US-003, US-004, US-008, US-012, US-014; global locked scope guardrails |
| Exact immutable snapshot model and schema version 1 | US-002, US-005, US-006, US-007, US-010 |
| Board/catalog/thread upstream acquisition and limits | US-003, US-004, US-007 |
| Memcached publication, activation, eviction, TTL, and cache-loss semantics | US-005, US-006, US-007 |
| Backend HTTP, configuration, health, and failure boundary | US-001, US-006 |
| Client IndexedDB startup, atomic synchronization, quota/schema failures, and reset | US-009, US-010 |
| Client cadence and private stable jitter | US-011 |
| PWA shell, manifest, Service Worker, and Cache Storage boundary | US-008, US-009 |
| Responsive catalog/thread UI, local-only navigation, lineage metadata, and degradation | US-012, US-014 |
| HTML trust boundary and canonical hyperlinks | US-013, US-014 |
| Direct, intent-gated, non-persistent media | US-015 |
| Linux-amd64 first-party images and hardening | US-016 |
| Compose topology, loopback-only edge, routing, ingress ownership, and operations | US-017 |
| Trace-first Go telemetry, minimal logs/metrics, Collector, and tail sampling | US-001, US-003, US-004, US-005, US-006, US-007, US-018 |
| Unit/integration-only testing and applicable build/lint/type checks | Every story's validation; decomposition principles |

All in-scope SEED concerns map to at least one proposed story. The explicitly unresolved implementation choices are listed under the relevant “Likely related architectural decisions” headings rather than silently assumed.
