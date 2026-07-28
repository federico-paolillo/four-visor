# Traceability Specialist Draft

## Scope and method

This draft independently checks `DECOMPOSE.md`, `AGENTS.md`, `docs/SEED.md`,
`.planning/ARCHITECTURE.md`, and `.planning/STORIES.md`. `docs/SEED.md` is
treated as the authoritative product specification. The proposed architectural
decisions are abbreviated as follows:

- **M1** — Store backend lineages as ordered fixed-size serialized blocks.
- **M2** — Store each browser lineage as one opaque IndexedDB record.
- **M3** — Enforce snapshot version 1 with boundary validators and shared
  fixtures.
- **M4** — Sanitize upstream HTML with a proven browser-side allowlist
  sanitizer.
- **M5** — Skip overlapping backend synchronization ticks.

The following interpretations in the architecture draft are used in the matrix:

- **I1 — Initial transport:** version 1 initially uses the one logical JSON
  response explicitly specified by `Snapshot transfer`; public fixed batches
  remain a later option only if measured size requires them.
- **I2 — Backend authority:** the client does not compare lineage identity or
  time, including when the served lineage ID equals the local ID.
- **I3 — Ordering and nesting:** reply nesting is presentation-only and never
  changes post sequence.
- **I4 — Failure versus absence:** a known attempted resource with a classified
  failure is `failed`; a resource never established by its parent remains
  absent.
- **I5 — Degradation:** the tolerance affects telemetry severity only and never
  gates activation.
- **I6 — Safe links:** unsafe schemes become non-clickable text; the clickable-
  link requirement does not override the untrusted-content boundary.
- **I7 — Commit boundary:** a pre-pointer-switch failure preserves the old
  lineage; a post-switch cleanup failure preserves the new lineage and relies
  on TTL cleanup.
- **I8 — Optional telemetry:** Collector/export failure does not fail unrelated
  application operations.

## Complete SEED section matrix

### Product definition and requirements

| SEED section | In-scope requirement or significance | Proposed mapping | Assessment |
| --- | --- | --- | --- |
| `Vision` | Read-only anonymous PWA; frozen backend-observed snapshots; personal-grade simplicity; no write, identity, personalization, search, bookmark, recommendation, or client-fetch expansion. | US-008, US-012, US-014; story draft `Locked scope guardrails`; no MADR. | Covered. Negative scope is a global guardrail rather than duplicated work. |
| `Personas / Reader` | Anonymous cached-snapshot browsing. | US-008–US-015. | Covered. |
| `Personas / Operator` | Deploy, monitor, and upgrade. | US-001, US-016–US-018. | Covered. |
| `Axioms` | Immutable independent lineages; one complete local lineage; backend authority; exact upstream order/HTML; text/media separation; visible degradation; ephemeral Memcached; browser serving; Preact/browser APIs; trace-first operation; operational state only. | M1–M5 where a mechanism remains open; US-002–US-015 and US-018; architecture `Axioms`, `Derived invariants`, and I2–I8. | Covered, except M2 currently violates the complete-old-lineage invariant for same-ID refresh; see T-001. |
| `Full Requirements / Product` | Read-only, anonymous, no product extras, original canonical URLs, exact ordering. | US-008, US-012–US-014; global guardrail. | Covered. Accessibility additions in US-012/US-014 are quality requirements from agent guidance, not SEED product expansion; this source should be made explicit (T-012). |
| `Full Requirements / Snapshot model` | Fresh immutable lineage, no prior-cache influence, old active until atomic replacement, no partial visibility, backend authority. | M1–M3; US-003–US-007, US-010; I2 and I7. | Covered in intent; M2's keying breaks atomicity in one required case (T-001). |
| `Full Requirements / Backend cache` | Every observed board; first 250 catalog threads; first 250 posts; oversize marker; no binaries; values unchanged except metadata. | US-002–US-005; M1 and M3. | Covered. |
| `Full Requirements / Client synchronization` | Approximately hourly stable 5–60-second installation jitter; complete staging and atomic activation; old-lineage continuity; one active lineage; transparent transport. | US-009–US-011; M2, M3; I1 and I2. | Covered in intent; same-ID staging is unsafe (T-001). |
| `Full Requirements / Local storage` | IndexedDB mandatory/exclusive for snapshots; Cache Storage shell-only; clear failure; total local reset; quota preserves current lineage. | US-008–US-010; M2. | Covered. |
| `Full Requirements / Rendering` | Backend retains HTML; frontend sanitizes; unsupported markup becomes text; external/canonical quote links; no raw injection. | M4; US-013–US-014; I6. | Covered. I6 is a necessary trust-boundary interpretation and is explicit. |
| `Full Requirements / Media` | Online thumbnail auto-load; explicit full media; ordinary browser cache only; placeholder/manual retry; spoilers hidden. | US-015. | Covered. |
| `Full Requirements / User interface` | Mobile-first responsive compact catalogs; nested/collapsible replies; always-visible lineage ID/age; visible failed/oversize states. | US-012, US-014; I3. | Covered, but US-012's History API behavior conflicts with locked no-routing scope (T-002). |
| `Full Requirements / Upstream acquisition` | Configurable hourly backend schedule; stable 5–60-second startup jitter; concurrency 10; global rate limit; five-second timeout; thirty-minute lineage deadline; transient-only bounded retry; exact User-Agent. | US-001, US-003, US-004, US-007; M5. | Partially explicit. The configurable interval/TTL relationship and unspecified rate/retry/tolerance defaults need objective choices (T-004). |
| `Full Requirements / Failure handling` | Failed known resources; absent unknown resources; degraded activation; pre-activation failure preserves old lineage; prominent degraded telemetry; all unfinished resources fail at deadline. | US-003–US-007, US-010, US-018; I4, I5, I7. | Mostly covered. Deadline completion is explicit for threads but not every known unfinished catalog/resource (T-005). |
| `Full Requirements / Deployment` | Compose; sole loopback edge; exact routing; separate internal services; ingress TLS; project-image hardening; `FOURVISOR_`; shallow health. | US-001, US-016, US-017. | Product coverage is complete. AGENTS' all-service port-range rule is not (T-003). |
| `Full Requirements / Platform and testing` | Linux amd64; Chrome Android 150+; unit/integration only. | US-008, US-016; every story's validation and global principles. | Covered, but the frontend validation task name contradicts AGENTS (T-006). |
| `Full Requirements / Observability` | Go OpenTelemetry boundary; sparse metrics/logs; request/sync roots and required children; successful sampling; failed retention. | US-001, US-003–US-007, US-018; I8. | Covered. US-001 and US-018 boundaries should be clarified to avoid duplicated telemetry ownership (T-009). |

### High-level architecture

| SEED section | In-scope requirement or significance | Proposed mapping | Assessment |
| --- | --- | --- | --- |
| `High-Level Architecture` | Client-first topology; only edge is browser-facing; client never fetches textual resources individually; backend builds and serves one lineage. | US-006, US-008–US-010, US-012, US-015, US-017. | Covered. |
| `Client architecture` | Mandatory IndexedDB; immediate local render; due-only refresh; inactive stage; validate then atomic swap; at most active plus incoming; quota continuity. | M2, M3; US-009–US-011. | Covered in intent; M2's physical key choice is inconsistent with this flow (T-001). |
| `Backend` | One process/cache/schedule; one active/building lineage; write-before-pointer; immediate eviction; twice-interval TTL; missing block is `410`. | M1, M5; US-005–US-007. | Covered. Configured interval must drive both scheduler and TTL explicitly (T-004). |
| `Single lineage authority` | Client accepts and never reconciles backend-selected lineage. | US-010; I2. | Covered. |
| `Snapshot contents` | Metadata, complete bounded text hierarchy, explicit failed/oversize resources, unchanged HTML/media references; no binaries or user/session state. | M3; US-002–US-004, US-015; global guardrail. | Covered. |
| `HTTP routing` | Exact edge order and `/api` stripping; only snapshot/health; status contracts; secret-free health body; no readiness route. | US-001, US-006, US-017. | Covered. |
| `Snapshot transfer` | One logical JSON response; internal Memcached blocks; ingress Brotli; no range/resume/resource/manifest/binary protocol. | M1; US-006, US-010, US-017; I1. | Covered. I1 should remain visibly labeled an interpretation rather than a new locked decision. |
| `Snapshot schema version 1` | Exact strict wrappers, types, states, cardinalities, ULID/UTC rules, opaque upstream objects, v1-only rejection. | M3; US-002, US-005, US-010. | Covered. M3's title should emphasize governance mechanism, not appear to decide the already locked schema (T-010). |
| `Upstream acquisition` | Scheduled-only bounded acquisition; exact defaults and fidelity; no repair/inference. | US-003, US-004, US-007; M5. | Covered subject to T-004/T-005. |
| `Media path` | Browser-to-4chan only; automatic thumbnails; explicit full media/spoiler reveal; ordinary cache; placeholder/manual retry. | US-015. | Covered. |
| `Observability path` | Required trace roots/children, minimal metric families, Collector tail policy. | US-001, US-003–US-007, US-018. | Covered. |

### Operational flows

| SEED section | In-scope requirement or significance | Proposed mapping | Assessment |
| --- | --- | --- | --- |
| `Client startup` | IndexedDB required; blocking clear failure; immediate active render; explicit empty state; schedule next sync. | US-009, US-011. | Covered. |
| `Client synchronization` | Due-only complete request; inactive staging; validation; atomic activation/cleanup; classified visible errors; next scheduled retry; backend authority. | M2, M3; US-010, US-011; I2. | Covered in intent; blocked by T-001. |
| `First installation jitter` | Random non-fingerprint seed in IndexedDB; never transmitted; stable until reset; 5–60-second offset. | US-009, US-011. | Covered. M2 currently includes the jitter store despite US-011 claiming no related decision (T-011). |
| `Scheduled backend synchronization` | Hourly stable instance jitter; new ULID; hard deadline; ordered acquisition; publication/activation/eviction; one build at a time. | M5; US-007, relying on US-003–US-005. | Covered subject to configuration clarity in T-004. |
| `Board acquisition` | Exact board order/data; transient-only retry; root failure activates `boards.failed`; no prior/inferred boards. | US-003, US-007. | Covered. |
| `Catalog acquisition` | Every known board; first 250 across preserved pages; failed versus absent semantics. | US-003; I4. | Covered except unfinished catalogs at lineage deadline are not objective (T-005). |
| `Thread acquisition` | Fetch each selected thread; 250 cap; oversize; retry classification; deadline failure; no remainder endpoint. | US-004. | Covered. |
| `Retry behavior` | Only network/timeout/rate limiting; `Retry-After`; global limiter; per-request/deadline bounds; no repair queue. | US-003, US-004. | Behavior covered; actual finite retry policy remains unspecified (T-004). |
| `Lineage construction and activation` | Complete blocks/metadata before pointer; failure preserves old active; immediate eviction; twice-interval TTL. | M1; US-005, US-007; I7. | Covered. |
| `Serving a snapshot` | Pointer/metadata/all blocks required before `200`; otherwise `410`; one logical response. | M1; US-006. | Covered. |
| `Degraded lineage completion` | Always activate resource-degraded valid lineages; tolerance only marks root error/log; include lineage/failure/tolerance attributes. | US-007, US-018; I5. | Covered, but tolerance default is absent (T-004). |
| `Client rendering` | Active local lineage only; no fetch/reconcile/inference/reorder/filter; visible failed/oversize; lineage ID/age. | US-012, US-014; I3. | Covered. |
| `Missing local resource` | Explicit unavailable message; zero backend fetch; canonical external option. | US-012, US-014. | Covered. |
| `Post markup rendering` | Isolated parse/strict allowlist; unsupported markup as text; original safe links; canonical quote links. | M4; US-013; I6. | Covered. |
| `Thumbnail loading` | Direct online request; placeholder on offline/error; unlimited manual retry; no explicit cache. | US-015. | Covered. |
| `Full media loading` | Explicit action; original media/native behavior; placeholder/manual retry; no proxy/transform/persist; spoiler reveal. | US-015. | Covered. |
| `Local reset` | Confirm; delete IndexedDB/app caches/seed/incoming; reload; local only. | US-009. | Covered. |
| `Backend component failure` | Required dependency fails operation; no fallback caches/stores/coordination; local lineage is continuity. | US-001, US-005, US-006, US-010, US-017; I8 for optional telemetry. | Covered. |
| `Health check` | Backend response, Memcached reachability, DNS only; no deep freshness/upstream-quality probe. | US-001, US-017. | Covered. |
| `Trace flow for inbound requests` | Request root; pointer/metadata/block/serialization children; error logs/status propagation; no routine logs. | US-001, US-006, US-018. | Covered. |
| `Trace flow for scheduled synchronization` | Sync root with lineage ID; board/catalog/thread/cache/activation/eviction children; completion event. | US-003–US-007, US-018. | Covered. |
| `Telemetry export` | Enumerated meaningful logs; minimal labels; no lineage/thread/URL/key/client/raw-error labels. | US-003–US-007, US-018. | Covered. |

### Deployment, rationale, and cross-cutting semantics

| SEED section | In-scope requirement or significance | Proposed mapping | Assessment |
| --- | --- | --- | --- |
| `Deployment View` | Five internal Compose components behind one ingress-to-loopback edge; backend-to-upstream and Collector export paths. | US-016–US-018. | Covered subject to T-003. |
| `Deployment philosophy` | Minimal single-node topology and client continuity. | US-017. | Covered. |
| `Deployment View / Backend` | Exactly one backend-owned Memcached instance. | US-005, US-017. | Covered. |
| `Container model` | Only edge host bind; frontend/backend/cache/Collector internal; first-party hardening only; native third-party config; `FOURVISOR_` only for Go. | US-001, US-016, US-017. | Covered subject to all-port rule (T-003). |
| `Health model` | Shallow three-part backend health. | US-001, US-017. | Covered. |
| `Traffic` | Exact ingress/edge/internal flow; backend text only; browser media direct. | US-006, US-015, US-017. | Covered. |
| `Scheduling` | Stable startup jitter followed by configured interval. | US-007. | Covered subject to T-004. |
| `Failure model` | Required components fail dependents; telemetry remains optional; specified single-service outcomes. | US-001, US-005–US-007, US-010, US-017, US-018; I8. | Covered. |
| `Security` | Ingress TLS; loopback edge; no internal exposure; private Memcached; first-party hardening; no enterprise claims. | US-016, US-017. | Covered subject to T-003. |
| `Deployment View / Observability` | Backend OTLP to Collector; third-party/Caddy stdout excluded; Collector sampling/export. | US-018. | Covered. |
| `Design Notes / Snapshot-first architecture` | Complete local snapshots, never intermediate. | M1–M3; US-005, US-010. | Covered. |
| `Design Notes / Client-first design` | IndexedDB is post-sync serving layer; offline text continuity. | M2; US-009, US-010, US-012, US-014. | Covered in intent; T-001 applies. |
| `Design Notes / Immutable lineages` | From scratch; no historical eligibility; complete-before-active. | US-003–US-007. | Covered. |
| `Design Notes / No incremental synchronization` | Complete replacement; no merge/differential logic. | US-010; I1. | Covered. |
| `Design Notes / Single backend` | No redundancy/coordination; client continuity. | US-005, US-007, US-017. | Covered. |
| `Design Notes / Memcached as a serving cache` | Pointer-selected ephemeral data; immediate deletion; TTL fallback; no durability. | M1; US-005, US-006. | Covered. |
| `Design Notes / Upstream fidelity` | Preserve ordering/semantics/HTML; add only cache metadata. | M3; US-002–US-004, US-012–US-014. | Covered. |
| `Design Notes / Binary exclusion` | Text only; browser ordinary media cache. | US-004, US-015. | Covered. |
| `Design Notes / Honest degradation` | Failed/oversize visible and degraded lineage telemetry. | US-003, US-004, US-007, US-012, US-014. | Covered. |
| `Design Notes / Browser platform first` | Preact only; direct browser APIs; no broad state/routing abstractions. | US-008–US-015. | Mostly covered; US-012's History API wording is inconsistent (T-002). |
| `Design Notes / Trace-first observability` | Root/child traces, sparse meaningful logs. | US-001, US-003–US-007, US-018. | Covered. |
| `Design Notes / Simplicity over flexibility` | No coordination, repair, warming, resume, personalization, or mutation. | M1–M5 choices; global guardrail. | Covered. |

### Detailed observability and failure semantics

| SEED section | In-scope requirement or significance | Proposed mapping | Assessment |
| --- | --- | --- | --- |
| `Detailed Observability / Philosophy` | Detailed traces, few metrics, sparse logs; each signal operationally useful. | US-001, US-018. | Covered. |
| `OpenTelemetry` | Only Go observability framework; backend OTLP to Collector; Collector receives/samples/exports. | US-001, US-018. | Covered; ownership overlap needs wording repair (T-009). |
| `Tracing` | Every request/sync root; HTTP/cache/lifecycle/serialization/validation children; useful attributes; no high-cardinality metric labels. | US-001, US-003–US-007, US-018. | Covered. |
| `Metrics / HTTP` | Server/client request counts and latency. | US-001, US-003, US-018. | Covered. |
| `Metrics / Cache` | Operations, hits, misses, errors, latency. | US-005, US-006, US-018. | Covered. |
| `Metrics / Lineages` | Duration, success/degraded outcomes, failed resources, active age. | US-007, US-018. | Covered. |
| `Logging` | Enumerated state/error events; exclude routine request/cache/outbound success chatter. | US-001, US-003–US-007, US-018. | Covered. |
| `Error handling` | Log, fail relevant spans/parents; excessive degradation attributes/log/root error. | US-001, US-003–US-007, US-018. | Covered. |
| `Sampling` | Collector retains all error traces and ~10% successful; no app sampling. | US-018. | Covered. |
| `Detailed Observability / Deployment` | Direct OTLP; no local buffering/secondary stack. | US-018. | Covered. |
| `Design principles` | Trace answers why, metrics health, logs event; emit less. | US-018 acceptance/documentation. | Covered. |
| `Failure Semantics / Philosophy` | No failover/repair/reconstruction; dependent operation fails; local lineage continuity. | US-005–US-007, US-010, US-017; global guardrail. | Covered. |
| `Backend component failures` | Exact outage effects for edge/frontend/backend/cache/upstream/Collector/ingress. | US-001, US-005–US-007, US-010, US-017, US-018; I8. | Covered. |
| `Client failures` | IndexedDB unavailable/corrupt; quota; network/backend; explicit schema mismatch. | US-009, US-010. | Covered. |
| `Synchronization failures` | Never activate partial; retain old; retry next scheduled interval. | US-010, US-011. | Covered. |
| `Lineage degradation` | Present/failed/oversize/absent; always activate valid degraded lineage; threshold changes telemetry only. | US-002–US-004, US-007; I4/I5. | Covered. |
| `Cache failures` | Missing active block set returns `410 Gone`. | US-006. | Covered. |
| `Media failures` | Placeholder/manual retry; textual availability unaffected; no automatic retry. | US-015. | Covered. |
| `Upstream failures` | Bounded transient retry; permanent/exhausted/unfinished become failed until next lineage. | US-003, US-004, US-007. | Partially explicit for unfinished non-thread work (T-005). |
| `Failure matrix` | Each listed visible outage outcome. | US-001, US-006, US-007, US-009, US-010, US-014, US-015, US-017. | Covered. |
| `Failure Semantics / Summary` | Client/backend outcomes for sync, upstream, storage, backend, and media failure. | US-007, US-010, US-011, US-015. | Covered. |
| `Operational principle` | Fail fast/visibly; preserve last complete client snapshot when possible. | US-001, US-005–US-007, US-009–US-011, US-015. | Covered. |

### Technology, locked decisions, and exclusions

| SEED section | In-scope requirement or significance | Proposed mapping | Assessment |
| --- | --- | --- | --- |
| `Technology Stack / Backend` | Go, `net/http`, Memcached, OpenTelemetry SDK/OTLP. | US-001, US-003–US-007, US-018. | Covered. |
| `Frontend` | Preact, Tailwind, TypeScript, ES modules, Vite/Vitest, direct IndexedDB/SW/Cache/Fetch. | US-002, US-008–US-015. | Covered. |
| `Data formats` | JSON, ingress Brotli, unchanged HTML, ULID. | US-002, US-006, US-007, US-013, US-017. | Covered. |
| `Infrastructure` | Docker/Compose/Caddy and first-party distroless/rootless/read-only. | US-016, US-017. | Covered. |
| `Networking` | HTTPS ingress; ingress Brotli; loopback edge; prefix stripping; internal services; upstream HTTP. | US-003, US-006, US-015, US-017. | Covered. |
| `Observability` | OTel/OTLP, Collector tail sampling, structured logs, metrics, traces. | US-001, US-018. | Covered. |
| `Testing` | Vitest and Go unit/integration only. | Every story validation; global principles. | Covered, except wrong frontend task name (T-006). |
| `Browser platform` | PWA/manifest/IndexedDB/SW/Cache Storage; History API not for app routing. | US-008–US-015. | Conflict in US-012 (T-002). |
| `Operating systems` | Linux amd64 and Chrome Android 150+. | US-008, US-016. | Covered. |
| `Configuration` | Go config only from `FOURVISOR_`. | US-001 and each backend story's documented configuration. | Coverage too broad/implicit in US-001; see T-004/T-007. |
| `Deliberate exclusions` | No alternate frameworks, databases, orchestration, queues, distributed cache, SSR. | Global guardrail and individual out-of-scope sections. | Covered. |
| `Technology Rationale / Philosophy` | Conservative, lightweight, personal-grade technology selection. | Architecture classification; all stories. | Covered; no duplicate MADR warranted. |
| `Go` | Small stdlib HTTP backend without framework. | US-001, US-003–US-007. | Covered. |
| `Memcached` | Disposable single serving cache, pointer namespacing, scheduled reconstruction. | M1; US-005–US-007. | Covered. |
| `Preact` | Sole narrow rendering abstraction; browser APIs primary. | US-008, US-012–US-015. | Covered. |
| `Tailwind CSS` | Vite-integrated styling. | US-008, US-012, US-014. | Covered. |
| `Vite` | Frontend build/dev tooling only. | US-008, US-016. | Covered. |
| `Vitest` | Frontend unit/integration validation only. | US-002, US-008–US-015. | Covered. |
| `IndexedDB` | Active lineage persistence and offline capability; exactly one active after sync. | M2; US-009–US-011. | Covered in intent; T-001/T-008 apply. |
| `Service Worker` | Shell only, offline startup, no snapshot Cache Storage. | US-008–US-010. | Covered. |
| `Docker Compose` | Minimal reproducible deployment, no Kubernetes. | US-017. | Covered. |
| `First-party container hardening` | Project-built images distroless/rootless/read-only; do not rebuild third-party images just to harden. | US-016, US-017. | Covered. |
| `OpenTelemetry` | Unified vendor-neutral Go signals, trace-first. | US-001, US-018. | Covered. |
| `Brotli-compressed JSON` | Ingress-supplied standard HTTP compression; no custom serialization. | US-006, US-017; I1. | Covered. |
| `Technology Rationale / Deliberate omissions` | Avoid systems solving excluded distributed/product concerns. | Global guardrail. | Covered. |
| `Locked Decisions / Product` | All fixed product identity and exclusions. | US-008, US-012–US-015; global guardrail. | Covered; must not be reopened. |
| `Locked Decisions / Snapshot model` | Exact v1, no compatibility, complete atomic backend-authoritative lineage. | M1–M3 mechanisms; US-002, US-005–US-007, US-010. | Covered in intent; T-001 applies. |
| `Locked Decisions / Backend cache` | Exact board/thread/post limits, oversize, no binary, unchanged HTML. | US-002–US-004. | Covered. |
| `Locked Decisions / Frontend` | Exact stack/target/storage/UI; no state framework/router. | US-008–US-015. | History conflict T-002. |
| `Locked Decisions / Rendering` | Sanitize, text fallback, clickable/canonical links, visible degradation. | M4; US-012–US-014; I6. | Covered. |
| `Locked Decisions / Synchronization` | Default hourly stable client/backend jitter; complete swap; one retained active lineage. | M2, M5; US-005, US-007, US-009–US-011. | Covered in intent; T-001/T-004 apply. |
| `Locked Decisions / Backend` | Exact backend/cache/topology/routing/compression/hardening/platform/configuration. | M1; US-001, US-005–US-007, US-016, US-017. | Covered subject to T-003/T-004. |
| `Locked Decisions / Testing` | Unit/integration only. | Global principles and every validation section. | Covered subject to T-006. |
| `Locked Decisions / Upstream` | Limiting/concurrency/timeout/deadline/retry/User-Agent. | US-003, US-004, US-007. | Covered behaviorally; policy values not supplied by SEED need explicit assumptions (T-004). |
| `Locked Decisions / Observability` | Go-only OTel contract; trace first; sparse metrics/logs; Collector tail retention. | US-001, US-018. | Covered. |
| `Locked Decisions / Failure semantics` | Required operation fails; resource-degraded activation; total outage; old lineage preservation; visible degradation. | US-001, US-003–US-007, US-010, US-018; I4/I5/I7/I8. | Covered except T-005. |
| `Locked Decisions / Out of scope` | No enterprise/multi-browser/arm64/distributed/incremental/client-fetch/media-cache/database/workflow/queue/Kubernetes work. | Global guardrail and story out-of-scope sections. | Covered. |
| `Out of Scope / User interaction` | No social/write/account/auth features. | Global guardrail; US-012/US-014 negative criteria. | Covered as exclusion, not work. |
| `Out of Scope / Personalization` | No preferences/settings/read/bookmark/search/recommendation/feed. | Global guardrail; US-011, US-012, US-014. | Covered as exclusion. |
| `Out of Scope / Snapshot behavior` | No incremental/partial/merge/differential/client fetch/history/version reconcile. | US-006, US-010, US-014; global guardrail; I1/I2. | Covered as exclusion. |
| `Out of Scope / Backend architecture` | No replicas/shared cache/coordination/workflow/repair/replay/guarantees. | M5; US-005, US-007, US-017; global guardrail. | Covered as exclusion. |
| `Out of Scope / Data storage` | No durable backend data/media/index stores or proxying. | US-005, US-015; global guardrail. | Covered as exclusion. |
| `Out of Scope / Media` | No server/offline media cache, automatic retry, transcoding/optimization/streaming infrastructure. | US-015. | Covered as exclusion. |
| `Out of Scope / Frontend` | No alternate framework/browser/router/SSR/MPA. | US-008, US-012; global guardrail. | Covered, except US-012's History wording risks app routing (T-002). |
| `Out of Scope / Testing` | No smoke/E2E/deployment tests. | Global principles and all validation sections. | Covered. US-016's artifact run is appropriately scoped as an image integration test. |
| `Out of Scope / Deployment` | No arm64/enterprise/Kubernetes/mesh/autoscaling/replication/consensus/stateful orchestration/complex probes. | US-016, US-017; global guardrail. | Covered. |
| `Out of Scope / Observability` | No verbose/high-cardinality/audit/analytics/tracking/replay. | US-001, US-003–US-007, US-018. | Covered. |
| `Out of Scope / Product philosophy` | Snapshot reader only, not social/archive/search/cache/distributed showcase. | Global guardrail; entire story set. | Covered. |

## Planning-task and agent-instruction matrix

| Source requirement | Draft mapping | Assessment |
| --- | --- | --- |
| Planning only; no production implementation. | Both specialist files contain proposals only. | Satisfied. |
| MADRs only for meaningful unresolved decisions; one decision each; no status fields. | Five proposed MADRs; architecture classification excludes axioms/locked choices. | Mostly satisfied. M3 title reads like a locked-contract decision (T-010), and M2 includes incidental jitter-store placement (T-011). |
| Stories independently implementable/reviewable, objective, observable, dependency ordered, with tests/observability/security in owning story. | 18 proposed stories with required sections and validation. | Mostly satisfied; speculative global configuration in US-001 (T-007), redundant edges (T-013), and trace ownership overlap (T-009) need cleanup. |
| No story depends on a later story. | Story dependency audit. | Satisfied; all references point earlier, though several transitive edges are redundant. |
| Unit/integration tests only; run exact Mise tasks. | Global principles and story validation. | Test types satisfied; frontend task incorrectly says `fe:typecheck` instead of `fe:check` (T-006). |
| Ports 65100–65199 for services and Docker; host exposure only explicit `127.0.0.1`; never alter firewall. | US-017 constrains only the published edge port and excludes firewall changes. | Partial; internal service/container ports are unmapped (T-003). |
| Final files are Coordinator-owned; specialist writes only temporary report. | This draft is only `.planning/TRACEABILITY_DRAFT.md`. | Satisfied. |

## Issues and recommended resolutions

### T-001 — Critical — M2 can overwrite or delete the active lineage before validation

**Evidence:** M2 keys the lineage store by `lineageId`, stages a candidate under
"its own lineage ID," and later changes the active pointer and deletes the
previous record. I2 and US-010 correctly require accepting the backend response
without identity comparison, including a same-ID refresh.

If incoming and active IDs match, the staging write targets the record already
named by the active pointer. It therefore exposes/replaces unvalidated content
before activation. The promotion transaction can then delete the "previous"
record, which is also the candidate/active record. This violates the locked
complete-old-lineage, no-partial-visibility, validation-before-activation, and
one-active-lineage requirements.

**Resolution:** revise M2 and US-009/US-010 so active and incoming records have
distinct local keys independent of the untrusted `lineageId` (for example,
role/generation keys or separate active/incoming stores). Treat `lineageId` as
validated payload metadata, not the sole storage identity. The activation
transaction must promote the distinct incoming record and remove the old record
without aliasing even when their lineage IDs match. Add an acceptance criterion
and integration case for a valid same-ID refresh plus failures before and during
that transaction.

### T-002 — High — US-012 introduces application navigation through History API

**Evidence:** US-012 scopes "board/thread selection with component state and
native browser history." The SEED says History API is used only for browser
history, not application routing; it also excludes client-side routing, and the
architecture describes history-independent navigation.

**Resolution:** remove the History API commitment from US-012 and use component
state for selection. If back/forward restoration is genuinely intended, record
a narrow explicit interpretation describing state-only history behavior and
prove it does not create route-addressable 4Visor content URLs. Do not turn the
locked no-router/no-application-routing decision into an open implementation
choice.

### T-003 — High — AGENTS' service-port range is only applied to the host bind

**Evidence:** US-017 requires the edge's published port to be 65100–65199 but
does not constrain frontend Caddy, backend, Memcached, Collector, or other
internal listener/container ports. AGENTS requires ports in 65100–65199 for
Docker Compose, containers, and services generally.

**Resolution:** add US-016/US-017 scope and acceptance criteria requiring every
configured listener and service port, including internal and third-party
service ports, to be in 65100–65199. Retain the stronger rule that only edge has
a host publication and it is explicitly bound to `127.0.0.1`. Validate all
rendered listener/target/health-check ports, not just published ports.

### T-004 — High — Required configurable policies are not closed by values or assumptions

**Evidence:** the SEED fixes concurrency 10, request timeout five seconds,
lineage deadline thirty minutes, and default interval one hour, but deliberately
does not give the numeric global rate, retry attempts/backoff, or degradation
tolerance. Architecture calls these story-level settings and says they must
become explicit configuration/defaults in acceptance criteria. US-003 and
US-007 only say "bounded," "configured," or "documented." US-001 claims all
configuration but cannot objectively validate settings owned by later stories.
The relationship between configured synchronization interval, actual cadence,
and M1/US-005's twice-interval TTL is also not an end-to-end criterion.

**Resolution:** in the owning stories, select and document personal-project
defaults for global request rate, retry count/backoff, and degradation tolerance
as explicit planning assumptions (or place genuinely unresolved values in
`OPEN_QUESTIONS.md`). Add objective criteria that a non-default
`FOURVISOR_` synchronization interval changes both scheduler cadence and cache
TTL, while the SEED-fixed defaults remain exact. Keep policy parsing with the
story that owns the behavior.

### T-005 — High — Deadline handling is explicit for threads but not all unfinished resources

**Evidence:** `Full Requirements / Failure handling` says unfinished resources
at the lineage deadline are marked failed. US-004 does this for queued/in-flight
threads. US-003 prevents attempts/retries crossing the deadline but does not say
that every known unfinished catalog becomes an exact failed wrapper. US-007's
hard deadline criterion does not close the representation.

**Resolution:** add to US-003 or US-007 that every known catalog/thread still
unfinished when the lineage deadline expires receives its exact `failed`
wrapper, while a board list that times out produces `boards.failed`; preserve I4
for genuinely unknown descendants. Add an integration case with unfinished
catalog and thread work in the same run.

### T-006 — High — The frontend validation command contradicts AGENTS

**Evidence:** story decomposition principles require `mise run fe:typecheck`.
AGENTS requires `mise run fe:check` alongside `fe:build`, `fe:test`, and
`fe:lint`.

**Resolution:** replace every proposed `fe:typecheck` reference with
`mise run fe:check`. Do not invent a new task name.

### T-007 — Medium — US-001's "all Go application configuration" is speculative and duplicated

**Evidence:** US-001 precedes acquisition, publication, scheduling, and
Collector work but claims to load and document all Go application configuration.
Those later stories own settings not yet selected, including acquisition and
degradation policy. This makes US-001 larger, less independently reviewable, and
duplicates configuration work in US-003/US-005/US-007/US-018.

**Resolution:** scope US-001 to a reusable `FOURVISOR_` parsing boundary plus
only health/Memcached/DNS/OTLP/server settings needed by that story. Each later
story adds and validates its own variables and defaults through the same
boundary. Keep the single source rule without speculative keys.

### T-008 — Medium — M2 assumes a monolithic mobile-browser record without a feasibility bound

**Evidence:** the maximum logical lineage can include every board, 250 catalog
threads per board, and 250 posts per fetched thread. M2 chooses one structured-
cloned IndexedDB value and acknowledges substantial memory/record size, but no
snapshot byte-size assumption, target fixture, or Chrome-for-Android feasibility
check supports the choice. The seed itself anticipates measured transfer size
becoming impractical.

**Resolution:** retain the simple one-record decision only with an explicit
personal-project size assumption and an early US-009 integration criterion that
stages, reads, and atomically promotes a representative upper-bound fixture on
the supported browser storage model. If no defensible bound can be stated,
choose a small fixed-record layout while preserving one logical lineage; do not
wait until UI implementation to discover the storage shape cannot work.

### T-009 — Medium — US-001 and US-018 have overlapping telemetry completion claims

**Evidence:** US-001 establishes SDK/OTLP, root spans, metrics, logging, and
export-failure behavior. US-018 then verifies roots, child structure, metrics,
logs, and failure non-interference across prior stories. This is mostly
incremental, but current wording lets either story be read as owning the same
end-to-end health telemetry.

**Resolution:** state that US-001 owns backend SDK/resource setup and health
instrumentation with an in-memory exporter; US-018 owns Collector configuration,
tail sampling/filtering, cross-capability signal consistency, and operator
export documentation. Retain US-018 verification but avoid reimplementing
instrumentation already accepted in earlier stories.

### T-010 — Medium — M3's title risks converting a locked contract into a proposed decision

**Evidence:** exact schema version 1 and two-boundary rejection are locked by
the SEED. The actual unresolved decision is independent idiomatic validators
plus shared fixtures, not whether to enforce v1.

**Resolution:** retitle M3 to something like "Govern cross-language snapshot
validation with independent validators and shared fixtures." In Context and
Decision, label exact v1 semantics as constraints and keep the decision limited
to governance/tooling. This avoids a duplicate MADR for a locked choice.

### T-011 — Low — M2 includes jitter-store placement but US-011 declares no related MADR

**Evidence:** M2's chosen direction places the installation-local jitter seed
in a metadata/settings store. US-011 implements that persistence but says no
MADR is related. The settings-store placement is also incidental to M2's one
decision about lineage representation.

**Resolution:** remove jitter-seed placement from M2 and leave it as a local
US-009/US-011 IndexedDB detail. Alternatively, list M2 under US-011 and explain
why the setting placement is inseparable; removal is the smaller, cleaner fix.

### T-012 — Low — Accessibility scope lacks explicit source traceability

**Evidence:** US-012 and US-014 correctly retain semantic and keyboard-operable
controls, but their SEED traceability implies these are SEED clauses. The SEED
does not state accessibility criteria; the governing agent guidance says not to
simplify accessibility basics away.

**Resolution:** keep the basic criteria and label them as an implementation-
quality assumption derived from AGENTS/developer guidance, not a new SEED
product feature.

### T-013 — Low — Several dependency edges duplicate transitive prerequisites

**Evidence:** US-007 lists US-003, US-004, and US-005 although US-005 already
depends on US-004, which depends on US-003. US-010 lists US-002 although both
US-006 and US-009 already depend on it. US-012 lists US-008 and US-009 although
US-010 already depends on US-009, which depends on US-008. US-018 lists US-001
and US-006 although US-017 already depends on both.

**Resolution:** list only direct prerequisites unless the final story template
explicitly defines dependencies as full transitive closure. Minimal direct
edges are: US-007 → US-005; US-010 → US-006 and US-009; US-012 → US-010;
US-018 → US-007 and US-017. Recheck whether US-007 needs a direct reference to
US-004 for orchestration clarity; it is not required for ordering.

## Duplicate-decision and duplicate-story audit

- M1, M2, M4, and M5 each address a distinct unresolved mechanism. M3 is also
  distinct in substance but needs T-010's title/scope repair so it does not look
  like a duplicate record of the locked v1 schema.
- M2's jitter-settings clause is incidental second work inside the storage-shape
  decision; T-011 removes it.
- US-002 defines/validates the contract; US-005 and US-010 consume that boundary.
  This is dependency reuse, not duplicate implementation, provided later stories
  do not recreate validators.
- US-003/US-004 acquire resources; US-007 orchestrates them. This is a valid
  layering of independently reviewable capability, not duplicate acquisition.
- US-005 publishes; US-006 reads/serves; US-007 schedules publication. These
  have distinct observable outcomes.
- US-008 owns shell cache lifecycle; US-009 invokes its deletion during local
  reset. This is required composition, not duplicate cache ownership.
- US-001/US-018 are the only material overlap. T-009 defines the boundary.
- No story duplicates excluded work, and no placeholder test/logging/cleanup
  story exists.

## Oversized-story audit

- **US-001:** coherent health vertical slice after removing speculative "all
  configuration" ownership per T-007.
- **US-003:** broad but cohesive board/catalog acquisition slice. Keep the
  shared outbound boundary here because it is immediately exercised; T-005
  must add the catalog deadline outcome.
- **US-007:** sizeable orchestration slice but still one observable scheduled
  lineage outcome. Do not split telemetry or degraded activation into cleanup
  stories.
- **US-010:** sizeable but necessarily atomic synchronization transaction; a
  split would hide the principal invariant. Fix M2 rather than splitting it.
- **US-017:** topology, routing, health wiring, and operator documentation form
  one deployable Compose slice. Add the port validation from T-003.
- **US-018:** remains a real Collector/export capability rather than a testing
  placeholder after applying T-009.
- The remaining stories are independently bounded and reviewable.

## Unsupported assumptions and interpretations

| Item | Status | Required action |
| --- | --- | --- |
| I1 one-response initial transport | Supported by the more specific `Snapshot transfer` text, despite the earlier general allowance for transparent fixed batches. | Keep explicit in final traceability; do not call public batching forbidden forever. |
| I2 same-lineage refresh accepted | Supported by unconditional backend authority and no ID/time comparison. | Keep; use it to repair/test M2 (T-001). |
| I3 visual-only nesting | Necessary reconciliation of exact upstream order with nested presentation. | Keep explicit. |
| I4 failed versus absent | Supported by schema and acquisition flows. | Keep and extend deadline handling (T-005). |
| I5 telemetry-only degradation threshold | Explicitly supported. | Keep. |
| I6 safe-scheme exception | Necessary security interpretation of clickable links. | Keep explicit with focused tests. |
| I7 pointer switch as commit boundary | Necessary and consistent with atomic activation/TTL cleanup. | Keep. |
| I8 telemetry export optional | Explicit in deployment failure model. | Keep. |
| DOMPurify choice | A supported new dependency decision, not named by the SEED. It is justified against custom/native/backend alternatives. | Keep as M4; final MADR should avoid claiming browser Sanitizer API support facts without current evidence. |
| One opaque IndexedDB record | Consequential but unsupported by any size/target feasibility evidence. | Resolve T-008 and T-001 before synthesis. |
| Global rate/retry/degradation defaults | Not supplied by SEED and not selected by drafts. | Resolve T-004 as explicit assumptions or open questions. |
| History API for selection | Unsupported and in tension with locked scope. | Remove or narrowly justify per T-002. |

## Unmapped or only partially mapped requirements

No complete top-level SEED section is omitted. The following individual
requirements are not yet mapped to objective, internally consistent story work:

1. **Atomic client replacement for a same-ID backend response** is contradicted
   by M2/US-009/US-010 rather than safely mapped (T-001).
2. **Every known unfinished resource becomes failed at the lineage deadline**
   is explicit only for threads, not unfinished catalogs (T-005).
3. **The backend synchronization interval is configurable and drives a TTL of
   twice that configured interval** lacks one cross-story acceptance condition
   connecting parsing, scheduling, and cache expiration (T-004).
4. **Numeric global request rate, bounded retry policy, and excessive-
   degradation tolerance** are seed gaps that require explicit planning
   assumptions or open questions before their stories are directly
   implementable (T-004).
5. **All Docker/container/service ports must be 65100–65199** from AGENTS is
   mapped only for the edge host publication, not internal listeners (T-003).
6. **The exact frontend validation task `mise run fe:check`** is incorrectly
   replaced by an unmandated task name (T-006).

## Dependency and ordering conclusion

No proposed story points to a numerically later story, so the current order is
topologically valid. It is not minimal: T-013 identifies redundant transitive
edges. US-016 could legally appear soon after US-006/US-008 rather than after all
reader UI work, but its current later placement is not an incorrect dependency.
US-018 correctly remains after both scheduled synchronization and Compose so it
can complete Collector-level integration.

## Final confidence

**Confidence: high (0.92) that all SEED sections were examined and all material
draft mappings were identified; medium-high (0.84) that the proposed plan is
repairable without changing product scope.** The drafts have broad and mostly
accurate coverage, no wholly omitted SEED area, and no later-pointing dependency.
They should not be synthesized unchanged: T-001 is a release-blocking atomicity
defect, and T-002 through T-006 contain locked-scope, operational-completeness,
or workflow violations that need repair. No Coordinator input is essential to
apply the recommended personal-project resolutions.
