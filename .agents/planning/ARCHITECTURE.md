# Architecture Specialist Findings

## Scope and conclusion

This analysis treats `docs/SEED.md` as authoritative and applies the MADR bar
from `DECOMPOSE.md`: a MADR is warranted only for an unresolved, consequential
architectural choice with credible alternatives. Axioms, locked requirements,
selected technologies, exclusions, and local coding choices are not promoted
into decisions merely to document them again.

The seed already fixes most of the target architecture. Five MADRs are
warranted:

1. Store backend lineages as ordered fixed-size serialized blocks.
2. Store each browser lineage as one opaque IndexedDB record.
3. Enforce the version 1 contract with boundary validators and shared fixtures.
4. Sanitize upstream HTML with a proven browser-side allowlist sanitizer.
5. Skip backend schedule ticks while a lineage build is active.

These five choices close the remaining system-level gaps without changing the
product model. Everything else below is classified as an axiom, an accepted
decision, a derived invariant, or an implementation detail.

## Architectural classification

### Axioms

The following are project-defining truths, not MADR candidates.

| Axiom group | Architectural meaning | Precise seed traceability |
| --- | --- | --- |
| Product identity | Read-only, anonymous PWA; no identity, write path, moderation, personalization, search, bookmarks, recommendations, or client-triggered acquisition. | `Vision`; `Axioms` bullets 1-4 and final local-state bullet; `Full Requirements / Product`; `Out of Scope / User interaction`, `Personalization`, and `Product philosophy` |
| Immutable lineage model | Each backend synchronization constructs a new immutable lineage independently from scratch; a client renders exactly one complete lineage and never sees a partial build. | `Axioms` bullets beginning “Every backend synchronization…”, “Every lineage…”, “Clients always…”, and “Clients never…”; `Full Requirements / Snapshot model`; `Design Notes / Snapshot-first architecture`, `Immutable lineages`, and `No incremental synchronization` |
| Authority and fidelity | The single backend chooses the active lineage. Neither tier ranks, filters, repairs, merges, reorders, or infers 4chan content. Upstream ordering and post HTML values are preserved. | `Axioms` bullets beginning “The backend is authoritative…”, “The backend never reasons…”, “Boards, catalogs…”, and “Original post HTML…”; `Operational Flows / Board acquisition`, `Catalog acquisition`, `Thread acquisition`, and `Client rendering` |
| Text/media separation | Backend lineages contain textual resources and media references only. Media retrieval is browser-to-4chan and independent of snapshot synchronization. | `Axioms` bullets beginning “The backend caches textual…”, “Images, videos…”, and “Browser media retrieval…”; `High-Level Architecture / Snapshot contents` and `Media path`; `Design Notes / Binary exclusion` |
| Honest degradation | Known acquisition failures remain explicit, oversize content remains visible, missing unknown content remains absent, and a resource-degraded lineage remains a valid snapshot. | `Axioms` bullets beginning “Failed resources…” and “Degraded lineages…”; `Full Requirements / Failure handling`; `Design Notes / Honest degradation`; `Failure Semantics / Lineage degradation` |
| Client continuity | The browser becomes the textual serving layer after synchronization, and its previous complete local lineage is the continuity mechanism during server or network failure. | `Axioms` bullets beginning “Client-side snapshots…”, “The browser is the primary…”, and “After synchronization…”; `High-Level Architecture / Client architecture`; `Design Notes / Client-first design` |
| Minimal operational model | One backend is authoritative, Memcached is disposable, and simplicity/determinism/transparency outrank completeness, recovery automation, and enterprise availability. | `Axioms` bullets beginning “One backend instance…”, “Memcached is…”, and “Simplicity…”; `Design Notes / Single backend`, `Memcached as a serving cache`, and `Simplicity over flexibility`; `Deployment View / Deployment philosophy` |
| Browser-platform ownership | Preact is the only frontend framework; IndexedDB, Service Worker, Cache Storage, Fetch, and browser lifecycle APIs are used directly, with no general state or routing layer. | `Axioms` bullet beginning “Preact is…”; `Design Notes / Browser platform first`; `Technology Stack / Frontend` and `Browser platform`; `Locked Decisions / Frontend` |
| Trace-first operation | Traces are the primary diagnostic signal; metrics are few and logs describe meaningful state changes and failures. | `Axioms` bullet “Observability is trace-first”; `Detailed Observability / Philosophy`, `Tracing`, `Metrics`, `Logging`, and `Sampling`; `Locked Decisions / Observability` |

### Locked or accepted decisions

These decisions are already made by the seed. They should appear in stories and
traceability, but creating MADRs for them would only restate the source.

| Accepted decision | What is already fixed | Precise seed traceability |
| --- | --- | --- |
| Snapshot contract | One nested JSON document, exact `schemaVersion: 1` wrappers and cardinalities, ULID lineage ID, UTC RFC 3339 observation time, strict wrapper fields, opaque upstream objects, no compatibility window. | `High-Level Architecture / Snapshot schema version 1`; `Locked Decisions / Snapshot model` |
| Public backend API | Browser routes are `GET /api/snapshot` and `GET /api/health`; edge Caddy strips `/api`; backend routes are `/snapshot` and `/health`; missing active data or any required block yields `410 Gone`. | `High-Level Architecture / HTTP routing`, `Snapshot transfer`, and `Backend`; `Operational Flows / Serving a snapshot`; `Locked Decisions / Backend` |
| Initial transfer form | Version 1 is exposed as one logical snapshot in one JSON response. Public manifests, range requests, resumable downloads, per-resource endpoints, and binary serialization are excluded. Fixed public batches are a future response only to measured size problems. | `High-Level Architecture / Snapshot transfer`; `Full Requirements / Client synchronization` |
| Backend cache lifecycle | A new lineage namespace is completed before one active pointer changes; the previous namespace is evicted after activation; lineage keys have a TTL of twice the configured interval as cleanup insurance. | `High-Level Architecture / Backend`; `Operational Flows / Lineage construction and activation`; `Design Notes / Memcached as a serving cache` |
| Acquisition contents | All observed boards, first 250 catalog threads in returned order, and first 250 returned posts per fetched thread are included; over-250 threads are truncated and marked `oversize`. | `Full Requirements / Backend cache`; `High-Level Architecture / Snapshot contents`; `Operational Flows / Catalog acquisition` and `Thread acquisition` |
| Acquisition envelope | Hourly configurable backend schedule, stable 5-60 second instance jitter, global rate limiting, default concurrency 10, five-second requests, thirty-minute lineage deadline, transient-only bounded retries, and commit-bearing User-Agent. | `Full Requirements / Upstream acquisition`; `High-Level Architecture / Upstream acquisition`; `Operational Flows / Retry behavior`; `Locked Decisions / Upstream` |
| Resource and lineage failure boundary | Known upstream request failures become resource states and do not block activation. Construction, contract validation, publication, cache write, and cancellation failures do block pointer replacement. | `Full Requirements / Failure handling`; `Operational Flows / Degraded lineage completion` and `Lineage construction and activation`; `Failure Semantics / Upstream failures`; `Locked Decisions / Failure semantics` |
| Browser synchronization semantics | Approximately hourly refresh with persisted installation-local 5-60 second jitter; stage the full candidate, validate it, atomically activate it, preserve the previous lineage on any failure, and accept the backend-selected lineage without timestamp comparison or reconciliation. | `Full Requirements / Client synchronization`; `Operational Flows / Client synchronization` and `First installation jitter`; `Locked Decisions / Synchronization` |
| Browser storage boundaries | IndexedDB is mandatory and exclusively holds snapshot data; Cache Storage holds only the application shell/static assets; reset clears both and the jitter seed; quota failure preserves the active lineage. | `Full Requirements / Local storage`; `High-Level Architecture / Client architecture`; `Operational Flows / Client startup` and `Local reset`; `Locked Decisions / Frontend` |
| Rendering and navigation | Raw post HTML stays unchanged at the backend and is sanitized only at render time; unsupported markup becomes text; external links remain clickable; quote links use canonical 4chan URLs; no missing-resource fetch occurs. | `Full Requirements / Rendering`; `Operational Flows / Client rendering`, `Missing local resource`, and `Post markup rendering`; `Locked Decisions / Rendering` |
| UI behavior | Mobile-first responsive UI, compact catalog rows, visually nested replies, collapsible posts, always-visible lineage ID and age, and visible failed/oversize states. | `Full Requirements / User interface`; `Locked Decisions / Frontend` and `Rendering` |
| Media behavior | Thumbnails auto-load online, original media loads only after user action, spoiler media stays hidden until reveal, failure shows a fixed placeholder, and retries are manual. No explicit application media cache exists. | `Full Requirements / Media`; `High-Level Architecture / Media path`; `Operational Flows / Thumbnail loading` and `Full media loading` |
| Deployment topology | Docker Compose with edge Caddy as the sole loopback host bind, separate internal frontend Caddy and Go backend, internal Memcached and Collector, ingress-owned TLS and Brotli, and no host exposure for internal services. | `Full Requirements / Deployment`; `Deployment View / Deployment philosophy`, `Container model`, `Traffic`, and `Security`; `Locked Decisions / Backend` |
| Runtime and technology choices | Go `net/http`, Memcached, OpenTelemetry/OTLP, Preact, TypeScript, Tailwind, Vite, Vitest, native browser APIs, JSON, Caddy, Docker Compose, Linux amd64, and Chrome for Android 150+ are selected. | `Technology Stack`; `Technology Rationale`; `Locked Decisions / Frontend`, `Backend`, and `Testing` |
| Container policy | Project-built images are distroless, rootless, and read-only; third-party images follow their supported runtime model. | `Full Requirements / Deployment`; `Deployment View / Container model` and `Security`; `Technology Rationale / First-party container hardening` |
| Configuration boundary | Go application settings come only from `FOURVISOR_` environment variables; Caddy, Memcached, and Collector retain their native configuration mechanisms. | `Full Requirements / Deployment`; `Deployment View / Container model`; `Technology Stack / Configuration`; `Locked Decisions / Backend` |
| Health semantics | Health is shallow: backend responsiveness, Memcached reachability, and 4chan DNS resolution only. The body is non-contractual and secret-free; there is no additional readiness route. | `High-Level Architecture / HTTP routing`; `Operational Flows / Health check`; `Deployment View / Health model` |
| Telemetry topology and policy | Go exports OTLP to the Collector; root traces cover inbound requests and scheduled synchronizations; child spans cover HTTP, cache, construction, activation, eviction, and internal work; Collector retains failed traces and about 10% of successful traces. | `Full Requirements / Observability`; `High-Level Architecture / Observability path`; `Detailed Observability`; `Locked Decisions / Observability` |
| Testing boundary | Unit and integration tests only; smoke, end-to-end, and deployment tests are excluded. | `Full Requirements / Platform and testing`; `Technology Stack / Testing`; `Locked Decisions / Testing`; `Out of Scope / Testing` |
| Explicit exclusions | Multi-backend coordination, durable backend storage, repair queues, incremental/differential synchronization, media proxying/storage, client routing, server rendering, other browsers/architectures, orchestration platforms, and product/social features are not planned. | `Technology Stack / Deliberate exclusions`; `Locked Decisions / Out of scope`; all subsections of `Out of Scope` |

### Genuinely unresolved architectural decisions

| Open choice | Why it is architectural | Resolution | MADR |
| --- | --- | --- | --- |
| Physical partitioning of a lineage in Memcached | Memcached item limits conflict with a potentially large logical JSON response. The choice affects publication, completeness checks, serving, eviction, and cache key count. | Serialize the complete v1 document and split its bytes into ordered fixed-size blocks described by completion metadata. | MADR 1 |
| Physical representation of lineages in IndexedDB | A monolithic record, normalized resource records, and client-side chunks have different transaction, query, and migration consequences. The seed fixes atomic semantics but not the storage shape. | Keep one complete candidate document per lineage record plus a small active-pointer/settings store. Promote and remove the old record in one transaction. | MADR 2 |
| Producer/consumer contract governance | Go produces a strict, nested contract while TypeScript must reject exact invalid shapes. The seed does not select JSON Schema, code generation, or independent validators. | Use explicit validators at both trust boundaries and one shared valid/invalid fixture corpus; avoid schema code generation and runtime schema engines for version 1. | MADR 3 |
| Ownership and mechanism of HTML sanitization | This is the primary untrusted-content boundary. A custom walker, browser Sanitizer API, backend sanitation, and a proven frontend sanitizer carry materially different security and fidelity risks. | Sanitize only in the browser with DOMPurify under an explicit allowlist, then canonicalize safe quote links before rendering. | MADR 4 |
| Behavior when a backend schedule tick overlaps a build | “One lineage under construction” rules out overlap but does not say whether a tick queues, cancels, or is discarded. The answer affects determinism and load. | Keep fixed cadence and skip an overlapping tick; do not queue or cancel work. | MADR 5 |

### Derived invariants that do not warrant MADRs

These follow necessarily from locked decisions and do not present a genuine
choice:

- `/api/snapshot` must be excluded from Service Worker shell caching and sent
  with HTTP caching disabled; otherwise textual snapshot data could escape
  IndexedDB or a stale intermediary response could override backend authority.
- Media URLs must bypass the backend and Service Worker application cache.
- The active Memcached pointer must be a single atomic value update. Distributed
  coordination is neither needed nor allowed with one backend.
- The backend must verify the active pointer, completion metadata, and presence
  of all required blocks before committing a `200` response; after response
  headers are written it can no longer truthfully return the required `410`.
- Reply nesting must be presentation-only: post array/DOM sequence remains in
  upstream order. A tree transformation that reorders replies is forbidden.
- A cleanup failure after a successful backend pointer switch cannot roll the
  lineage back. It leaves old inactive keys for TTL cleanup and emits an error.
  The “preserve current active lineage” rule applies to failures before the
  activation commit.
- “Stored exactly as received” means preservation of upstream field names,
  values, array order, page boundaries, and HTML string values. It does not
  require byte-for-byte preservation of upstream JSON serialization.
- A degraded lineage activates only if lineage construction, validation,
  publication, and pointer activation themselves succeed. Resource failure and
  lineage publication failure are intentionally different classes.

### Implementation details, not MADRs

The following choices should be made inside the relevant stories and verified
by unit or integration tests. They are local, reversible, or already constrained
enough by the seed:

- Go package names, interfaces, worker-pool mechanics, HTTP handler layout, and
  dependency wiring.
- Memcached key spelling, exact block byte size below the configured item limit,
  serialization buffers, multi-get usage, and eviction batching.
- Exact `FOURVISOR_` variable names, parsing helpers, and configuration struct
  layout. Defaults explicitly stated by the seed remain mandatory.
- The numeric global request rate, bounded retry count/backoff, and excessive
  degradation tolerance. These must become explicit configuration/defaults in
  story acceptance criteria, but changing them does not alter the architecture.
- Retry jitter formula, random source, stable backend jitter persistence, and
  cancellation implementation.
- IndexedDB database/store names, database version number, wrapper functions,
  inactive-candidate cleanup, and the installation-seed encoding.
- Preact component boundaries, state hooks, Tailwind classes, collapsed-post
  ephemeral state, and the exact visual indentation heuristic for replies. The
  heuristic may derive a parent from quote links but must not reorder posts.
- The precise sanitization element/attribute allowlist and CSS presentation of
  unsupported markup. They implement MADR 4 and require focused malicious-input
  tests.
- Service Worker cache names, precache manifest generation, application-shell
  update strategy, and offline fallback component structure.
- Health response body shape, dependency probe timeout, and internal diagnostic
  text, subject to the locked disclosure rule.
- OpenTelemetry span names, instrumentation helpers, Collector YAML, exporter
  endpoint variables, and the exact low-cardinality metric names.
- Dockerfile stage names, numeric container users, Compose service names, and
  internal ports, subject to `AGENTS.md` port and host-bind constraints.
- Unit/integration test file organization and fixture serialization.

## Proposed MADRs

### MADR 1 — Store backend lineages as ordered fixed-size serialized blocks

#### Context

The public contract is one nested JSON response, while Memcached values have a
per-item ceiling and a full lineage can be much larger than one item. The seed
requires all blocks and completion metadata to exist before activation, and a
missing required block must produce `410 Gone`. It deliberately leaves the
internal block layout open.

#### Chosen direction

Serialize the exact schema-version-1 logical JSON document to UTF-8 and divide
that byte stream into ordered fixed-size blocks, each safely below the deployed
Memcached item ceiling. Store immutable blocks under the lineage namespace.
Store completion metadata containing at least the ordered block count and total
byte length. The active pointer continues to contain only the lineage identity.

Before returning `200`, the snapshot handler resolves completion metadata and
confirms every block is present. It then concatenates/streams blocks in order as
one JSON entity. The precise safe block size and key spelling are implementation
details.

#### Decision drivers

- Remain below Memcached per-item limits.
- Preserve the exact one-response public contract.
- Make atomic publication a single pointer change.
- Detect incomplete/evicted active lineages deterministically.
- Keep the cache representation independent of board/thread structure.
- Avoid a database, public block protocol, or custom binary format.

#### Considered options

1. **Ordered fixed-size serialized blocks — chosen.** Minimal reconstruction
   logic and no oversized resource special case.
2. **One Memcached value per lineage.** Simplest, but cannot safely satisfy
   ordinary item ceilings for the required data volume.
3. **Resource-aligned board/catalog/thread blocks.** Natural domain keys, but
   creates many cache operations, still needs subdivision for a large resource,
   and couples serving to schema assembly.
4. **Persistent file/database backing.** Avoids item limits but contradicts the
   locked ephemeral-cache and no-durable-backend-state architecture.

#### Consequences

Positive:

- One generic chunking rule handles every lineage size up to overall capacity.
- Public clients remain unaware of cache partitioning.
- Completion is easy to determine from immutable metadata and block presence.
- Schema evolution, if ever authorized, does not require a new cache topology.

Negative:

- Individual resources are not independently readable from cache.
- The backend must finish serialization before publication.
- Serving must resolve/check all blocks before committing the HTTP success
  status, increasing per-request memory or cache-operation pressure.
- Memcached still provides no durability; eviction correctly produces `410`.

#### Related story capabilities

- Build and validate a lineage.
- Publish and atomically activate a lineage in Memcached.
- Serve the active logical snapshot and return `410` for incomplete cache data.
- Evict the previous lineage and retain TTL cleanup insurance.
- Exercise cache publication/read failure paths with integration tests.

#### Precise SEED traceability

- `Axioms`: immutable lineages; Memcached is an ephemeral serving cache.
- `Full Requirements / Snapshot model`: atomic activation and no partial
  visibility.
- `Full Requirements / Backend cache`: complete textual resource scope.
- `High-Level Architecture / Backend`: lineage blocks, active pointer, TTL,
  missing block behavior.
- `High-Level Architecture / Snapshot transfer`: one logical JSON response;
  internal multi-block storage permitted.
- `Operational Flows / Lineage construction and activation`: write every block,
  completion metadata, pointer switch, eviction.
- `Operational Flows / Serving a snapshot`: all blocks required or `410 Gone`.
- `Design Notes / Memcached as a serving cache`.

### MADR 2 — Store each browser lineage as one opaque IndexedDB record

#### Context

The browser downloads and validates one complete nested document and renders
from one active local lineage. IndexedDB must hold both the current active
lineage and, transiently, one inactive incoming candidate. The seed fixes the
activation semantics but not whether the client normalizes resources, stores
transport chunks, or stores the logical document intact.

#### Chosen direction

Use a lineage object store keyed by `lineageId`, with one value containing the
entire received version-1 document, and a small metadata/settings store holding
the active lineage ID and installation-local jitter seed.

Write a downloaded candidate under its own lineage ID without changing the
active pointer. Validate the staged candidate. Promote it by one IndexedDB
read-write transaction that changes the active pointer and deletes the previous
lineage record; transaction failure leaves the previous pointer and record
intact. Rendering reads only the record named by the committed active pointer.

Do not normalize boards/threads or mirror backend transport blocks until
measured Chrome-for-Android storage or structured-clone behavior demonstrates a
real problem.

#### Decision drivers

- Match the one-document transfer and immutable-lineage mental model.
- Make activation and old-lineage removal transactionally simple.
- Ensure inactive or invalid data is never renderable.
- Minimize schema-specific IndexedDB code and migration surface.
- Keep the personal-project implementation small.

#### Considered options

1. **One record per lineage — chosen.** Directly mirrors the contract and needs
   only a pointer transaction.
2. **Normalize boards, catalogs, threads, and posts into stores.** Enables
   selective reads but creates indexes, joins, bulk cleanup, and schema-coupled
   migration logic that the initial client does not require.
3. **Store fixed transport/cache blocks.** Could reduce per-record size but
   leaks a replaceable transport concern into browser persistence and rendering.
4. **Cache Storage or in-memory storage.** Directly violates mandatory storage
   boundaries and offline continuity.

#### Consequences

Positive:

- Storage shape is identical to the validated contract.
- Atomic promotion has one clear transaction boundary.
- Reset and failed-candidate cleanup are straightforward.
- No client-side entity reconstruction or lineage reconciliation exists.

Negative:

- Reading or writing a lineage structured-clones a large value.
- The active document may occupy substantial browser memory while rendering.
- Very large measured snapshots may later justify fixed records or normalized
  stores and an IndexedDB schema migration.
- Validation code must avoid accidentally rendering the inactive record.

#### Related story capabilities

- Initialize mandatory IndexedDB storage and surface storage failure.
- Persist and reuse the installation-local jitter seed.
- Synchronize, stage, validate, and atomically activate a lineage.
- Preserve the active lineage on network, schema, quota, and transaction errors.
- Reset all local snapshot and application-shell data.
- Render boards and threads only from the active local lineage.

#### Precise SEED traceability

- `Axioms`: clients render one complete local lineage; browser is primary serving
  layer; operational local state only.
- `Full Requirements / Client synchronization` and `Local storage`.
- `High-Level Architecture / Client architecture`.
- `High-Level Architecture / Snapshot transfer` and `Snapshot schema version 1`.
- `Operational Flows / Client startup`, `Client synchronization`, `First
  installation jitter`, and `Local reset`.
- `Design Notes / Client-first design` and `No incremental synchronization`.
- `Failure Semantics / Client failures` and `Synchronization failures`.

### MADR 3 — Enforce snapshot version 1 with boundary validators and shared fixtures

#### Context

The Go producer and TypeScript consumer must agree on a strict contract. Wrapper
objects reject unknown fields and invalid state/payload combinations, while
upstream board, summary, metadata, and post objects remain opaque. The seed
requires backend validation before publication and client validation before
activation, but does not choose a cross-language contract mechanism.

#### Chosen direction

Implement explicit version-1 validators in Go at lineage publication and in
TypeScript at the network/storage activation boundary. Keep the validators
independent and idiomatic to each language. Maintain one shared, language-neutral
fixture corpus containing representative valid documents and focused invalid
documents for every strict wrapper rule; both validation suites consume the
same corpus.

Do not introduce JSON-Schema runtime engines, schema code generation, or a
compatibility/migration layer for version 1. Opaque upstream objects receive
only the object-type check required by the seed, while contract-owned wrappers
receive exact validation.

#### Decision drivers

- Enforce the exact rejection behavior in both runtimes.
- Prevent drift without adding generators and runtime schema dependencies.
- Preserve unrestricted upstream fields and values.
- Keep validation failures classified at the correct trust boundaries.
- Honor the explicit no-migration/no-compatibility-window decision.

#### Considered options

1. **Independent validators plus shared fixtures — chosen.** Small tooling
   footprint with executable cross-language agreement.
2. **Canonical JSON Schema with runtime validators.** Centralizes declaration
   but adds runtime libraries in both tiers and still requires careful opaque
   object handling.
3. **Generate Go and TypeScript models/validators from a schema.** Reduces some
   duplication but introduces a build-time toolchain and generated-code
   lifecycle disproportionate to one frozen version.
4. **Trust backend output or validate only `schemaVersion`.** Too weak for the
   seed's explicit wrapper, cardinality, timestamp, ULID, and state rules.

#### Consequences

Positive:

- Each boundary fails invalid data before it becomes active.
- Shared negative fixtures expose producer/consumer interpretation drift.
- No new runtime validator or code-generation stack is required.
- Upstream payload evolution remains accepted inside opaque objects.

Negative:

- Validation logic is intentionally duplicated across Go and TypeScript.
- Every contract change would require two validator edits and fixture updates.
- Fixture coverage must be maintained carefully because it is the shared
  executable specification.

#### Related story capabilities

- Define version-1 domain/transport models without constraining opaque upstream
  payloads.
- Validate a completed backend lineage before cache publication.
- Validate a staged browser candidate before activation.
- Preserve current active lineages on construction or schema failure.
- Cover valid and invalid contract shapes in both unit-test suites.

#### Precise SEED traceability

- `Full Requirements / Snapshot model`, `Client synchronization`, and `Failure
  handling`.
- `High-Level Architecture / Snapshot schema version 1` in full.
- `Operational Flows / Client synchronization` and `Lineage construction and
  activation`.
- `Detailed Observability / Error handling`.
- `Failure Semantics / Client failures`, especially schema mismatch.
- `Locked Decisions / Snapshot model` and `Failure semantics`.

### MADR 4 — Sanitize upstream HTML with a proven browser-side allowlist sanitizer

#### Context

Post HTML is intentionally stored unchanged and is untrusted. The PWA must
preserve supported markup and safe links, turn unsupported markup into text,
canonicalize quote navigation to 4chan, and never place unsanitized HTML in the
main document. This security boundary is consequential enough that a bespoke
parser is not the minimal safe choice.

#### Chosen direction

Use DOMPurify in the frontend with an explicit, minimal element/attribute
allowlist for supported 4chan post markup. Preserve the textual content of
unsupported elements while removing their element semantics. Remove event
handlers, styling injection, active content, and unsafe URL schemes. After
sanitization, canonicalize recognized quote links to HTTPS 4chan thread/post
URLs using the containing board/thread context; retain other safe HTTP(S)
destinations. Render only the resulting sanitized value.

The backend continues storing the upstream HTML string unchanged. Unsafe links
degrade to non-clickable text; the requirement to keep hyperlinks clickable
does not override the trust-boundary requirement.

#### Decision drivers

- Treat upstream HTML as hostile at the browser boundary.
- Avoid maintaining a custom HTML sanitizer.
- Preserve safe supported markup and visible text.
- Keep canonical navigation outside 4Visor.
- Leave the backend representation faithful to upstream data.
- Fit the Chrome-for-Android-only target without relying on an immature browser
  Sanitizer API contract.

#### Considered options

1. **DOMPurify with an explicit allowlist — chosen.** Mature and narrowly scoped;
   custom logic is limited to 4chan markup and quote-link policy.
2. **Custom `DOMParser` tree walker.** Avoids a dependency but makes the project
   responsible for subtle element, attribute, URL, namespace, and mutation-XSS
   behavior.
3. **Native Sanitizer API.** Attractive platform ownership, but its exact
   support/contract is not part of the locked target and would couple safety to
   a less established API.
4. **Sanitize in the backend.** Violates unchanged backend storage, shifts the
   trust boundary, and risks conflating cache fidelity with presentation.
5. **Render raw HTML.** Explicitly forbidden and unsafe.

#### Consequences

Positive:

- The riskiest content boundary uses a maintained security-focused component.
- Backend snapshots remain faithful and reusable as specified.
- Safe link and unsupported-markup behavior can be tested independently from UI
  layout.
- No custom general-purpose HTML parser/sanitizer is introduced.

Negative:

- DOMPurify becomes an additional frontend dependency that must be updated.
- The allowlist and quote-link transformation remain project-owned policy.
- Malicious or unsupported link schemes cannot remain clickable.
- Sanitizer output may not reproduce every visual detail of upstream HTML.

#### Related story capabilities

- Render post bodies safely from unchanged upstream HTML.
- Preserve supported formatting and visible unsupported text.
- Keep external safe links clickable and rewrite quote links canonically.
- Render nested/collapsible posts without unsafe DOM insertion.
- Verify malicious markup, attributes, URLs, unsupported elements, and quote
  links with unit tests.

#### Precise SEED traceability

- `Axioms`: original post HTML is stored unchanged; Preact/browser ownership.
- `Full Requirements / Rendering`.
- `High-Level Architecture / Snapshot contents`.
- `Operational Flows / Client rendering` and `Post markup rendering`.
- `Design Notes / Upstream fidelity` and `Browser platform first`.
- `Locked Decisions / Rendering`.
- `Technology Stack / Frontend` and `Data formats`.

### MADR 5 — Skip overlapping backend synchronization ticks

#### Context

The scheduler follows a configured fixed interval after a stable startup jitter,
and a lineage build can run for up to thirty minutes. The backend owns only one
lineage under construction. A short configured interval or delayed build can
therefore cause a new tick while work is active. The seed excludes distributed
coordination and repair queues but does not say whether the local tick queues,
cancels, or is discarded.

#### Chosen direction

Run lineage construction as a single-flight operation. Keep the fixed schedule;
when a tick occurs during an active build, skip that tick. Do not queue a later
run, overlap builds, or cancel the current build. The next ordinary scheduled
tick is the next opportunity. Record the skip as a meaningful scheduler state
event using the existing observability path, without adding a queue or retry
subsystem.

#### Decision drivers

- Preserve exactly one lineage under construction.
- Avoid competing upstream load and cache publication races.
- Keep cancellation semantics tied to shutdown/deadline, not freshness guesses.
- Avoid queues and accumulated catch-up work.
- Keep scheduling deterministic and understandable for one instance.

#### Considered options

1. **Skip overlapping ticks — chosen.** Maintains fixed cadence and bounded work
   with the least state.
2. **Queue one pending synchronization.** Reduces missed opportunities but can
   cause immediate catch-up load and introduces queue state.
3. **Cancel the current build and start the newer tick.** Wastes acquisition
   work and can starve activation when builds repeatedly run long.
4. **Allow concurrent builds.** Contradicts the one-building-lineage model and
   introduces activation races and greater upstream load.
5. **Schedule the next interval only after completion.** Avoids overlap but
   changes fixed cadence and makes refresh frequency depend on build duration.

#### Consequences

Positive:

- At most one acquisition tree consumes resources or publishes cache data.
- No queue, coordinator, generation race, or cancellation handoff is needed.
- The current active lineage remains served throughout a long build.
- Default settings normally avoid the edge case because the one-hour interval
  exceeds the thirty-minute deadline.

Negative:

- Misconfigured short intervals can result in skipped refresh opportunities.
- A failed or slow build is not followed by an immediate catch-up attempt.
- Operators must use trace/log evidence to distinguish an overlap skip from a
  started synchronization.

#### Related story capabilities

- Configure and run the stable-jitter backend scheduler.
- Enforce one active lineage construction with deadline cancellation.
- Start scheduled synchronization root traces and record skipped overlap events.
- Preserve the active lineage while construction is in progress or fails.
- Unit-test overlapping ticks without smoke or end-to-end tests.

#### Precise SEED traceability

- `Axioms`: each synchronization is independent; one backend is authoritative;
  simplicity and determinism take precedence.
- `Full Requirements / Upstream acquisition`.
- `High-Level Architecture / Backend` and `Upstream acquisition`.
- `Operational Flows / Scheduled backend synchronization` and `Retry behavior`.
- `Deployment View / Scheduling`.
- `Design Notes / Single backend` and `Simplicity over flexibility`.
- `Locked Decisions / Synchronization`, `Backend`, and `Out of scope`.

## Non-MADR interpretations needed by stories

These statements resolve apparent ambiguity without introducing new
architectural records:

- **Reply nesting and ordering:** render posts in the exact upstream sequence.
  Indentation or reply-parent cues may be derived from quote links, but a reply
  tree must not move a post before or after another post.
- **Unsupported HTML:** preserve readable text, not unsupported element behavior
  or unsafe literal markup. Supported safe descendants may remain formatted only
  after the sanitizer accepts them.
- **Same-lineage refresh:** the client accepts and may replace from whatever
  complete active lineage the backend serves, even if the lineage ID matches
  local state. It does not use identity or time to overrule backend authority.
- **No local snapshot:** first installation shows the specified empty state and
  starts the ordinary jittered synchronization schedule; it does not fetch
  individual resources or use an online-only mode.
- **Unknown absence versus technical failure:** only a known attempted resource
  with a classified acquisition failure receives `failed`; a resource that was
  never established by its parent response remains absent.
- **Degradation threshold:** it changes telemetry severity only. It never gates
  lineage activation.
- **OpenTelemetry outage:** telemetry export is optional to application work;
  its failure cannot fail snapshot serving or lineage construction.

## Internal consistency review

### Resolved tensions

1. **One response versus possible fixed batches.** The detailed transfer section
   is specific: the initial contract is one JSON response. The earlier “may use
   one payload or multiple fixed batches” language preserves a future option,
   not an unresolved initial implementation choice. A later switch to public
   blocks would need its own MADR and contract work after measurement.
2. **Stage before validate.** The staged IndexedDB candidate is inactive and
   unreachable by rendering. Validation occurs before the active pointer
   transaction, so the flow does not expose invalid data.
3. **Degraded activation versus failed construction.** Resource failures produce
   valid `failed` wrappers; structural validation, cache publication, or pointer
   failures prevent activation. The two rules apply at different boundaries.
4. **Immediate eviction versus preserving the current lineage on failure.** The
   commit point is the active-pointer switch. Pre-commit failures preserve the
   old active lineage. Post-commit eviction failure preserves the new active
   lineage and lets TTL clean old inactive keys; rollback would be less atomic.
5. **Exact upstream fidelity versus JSON production.** Field/value/order fidelity
   is required, but JSON object byte order/whitespace is not. This permits normal
   decoding and version-1 serialization while keeping HTML string content
   unchanged.
6. **Nested replies versus strict post order.** Nesting is visual metadata/layout,
   not a reordered tree traversal.
7. **Clickable links versus untrusted URLs.** Only safe navigable schemes can be
   clickable. Security at the render trust boundary necessarily wins over
   preserving an unsafe `href` behavior.
8. **Application unavailable versus offline continuity.** A new shell request
   fails when ingress/edge/frontend is down, while an already installed cached
   shell can still open and render its IndexedDB lineage. The failure tables
   describe both cases consistently.
9. **“Every board” versus failed board-list acquisition.** Every board observed
   in a successful board-list response is processed. When that root request
   fails, `boards.state: failed` is itself the complete degraded representation;
   the backend does not invent board identities.
10. **Only one active browser lineage versus staging.** The candidate remains
    inactive. MADR 2's pointer/delete transaction leaves one active lineage after
    commit and preserves the old one on transaction failure.

### Coverage check

| SEED area | Classification and coverage |
| --- | --- |
| `Vision` and `Personas` | Product axioms and capability drivers; no MADR. Reader stories cover browsing/synchronization/rendering; operator stories cover deployment, health, configuration, and observability. |
| `Axioms` | Fully classified in the axiom table. No axiom is restated as a MADR. |
| `Full Requirements / Product` | Locked product scope; no architectural choice remains. |
| `Full Requirements / Snapshot model` | Locked semantics; physical server/client representations resolved by MADRs 1 and 2; contract governance resolved by MADR 3. |
| `Full Requirements / Backend cache` | Locked content/cardinality rules; MADR 1 covers only physical block layout. |
| `Full Requirements / Client synchronization` | Locked activation behavior; MADR 2 covers storage representation and MADR 3 covers validation. |
| `Full Requirements / Local storage` | Locked platform/storage boundary; MADR 2 covers only unresolved record shape. |
| `Full Requirements / Rendering` | Locked fidelity/safety outcome; MADR 4 resolves sanitizer ownership/mechanism. |
| `Full Requirements / Media` | Fully locked behavior; no MADR. |
| `Full Requirements / User interface` | Product presentation requirements; reply-order interpretation recorded above; remaining work is story-level UI implementation. |
| `Full Requirements / Upstream acquisition` | Locked acquisition envelope; MADR 5 resolves overlapping ticks; exact local algorithms/defaults not supplied remain implementation details. |
| `Full Requirements / Failure handling` | Locked and reconciled by failure boundary; MADRs 1-5 preserve those semantics. |
| `Full Requirements / Deployment` | Topology, routing, hardening, configuration source, and health dependencies are locked; no MADR. |
| `Full Requirements / Platform and testing` | Locked target/test boundary; no MADR. |
| `Full Requirements / Observability` | Locked signal ownership and sampling; no MADR. |
| `High-Level Architecture` | Component topology is accepted. Backend physical blocks map to MADR 1, client records to MADR 2, schema enforcement to MADR 3, render boundary to MADR 4, and scheduling overlap to MADR 5. |
| `Operational Flows` | Every flow is either an accepted behavioral sequence, a related capability named by a proposed MADR, or a local implementation detail listed above. No flow requires an additional component. |
| `Deployment View` | Accepted Compose topology/security/failure model; scheduling overlap maps to MADR 5. |
| `Design Notes` | Rationale for axioms and locked choices; not independent decision requests. |
| `Detailed Observability` | Accepted telemetry architecture. Exact names/configuration are implementation details. No duplicate MADR. |
| `Failure Semantics` | Accepted failure taxonomy; resolved tensions documented above. No missing recovery architecture is inferred. |
| `Technology Stack` and `Technology Rationale` | Accepted technology selections. MADR 4 adds only the security-focused sanitizer needed to implement an unresolved trust boundary. |
| `Locked Decisions` | Treated as accepted by definition. None is duplicated as a proposed MADR. |
| `Out of Scope` | Treated as exclusions, never as negative MADRs or implementation stories. |

## Final validation

- **Meaningful decision test:** each proposed MADR has at least two viable
  in-scope alternatives and consequences that cross a component or trust/
  transaction boundary.
- **No-restatement test:** none chooses Preact, Go, Memcached, IndexedDB, JSON,
  Caddy, Compose, OpenTelemetry, immutable lineages, one backend, or any other
  already locked selection.
- **One-decision test:** backend cache partitioning, browser storage shape,
  contract governance, HTML sanitization, and scheduler overlap are separate.
- **Scope test:** no accounts, write paths, search, personalization, media cache,
  durable backend store, incremental sync, alternate route, repair queue,
  browser target, or deployment platform has been introduced.
- **Consistency test:** every chosen direction preserves atomic activation,
  backend authority, upstream order, visible degradation, last-client-lineage
  continuity, and personal-project simplicity.
- **Coverage test:** every top-level and requirement subsection in the seed is
  accounted for in the classification or coverage table; no architectural gap
  requires coordinator input.
