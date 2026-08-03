# Traceability

## Scope and method

This final matrix maps the authoritative `docs/SEED.md` requirements to final
MADRs, final User Stories, `docs/TODO.md` assumptions, and the resolved
interpretations below. `DECOMPOSE.md` and `AGENTS.md` govern artifact and
workflow quality. The final architectural decisions are abbreviated as follows:

- **M1** — Store backend lineages as ordered fixed-size serialized blocks.
- **M2** — Store browser lineages as locally keyed fixed-size records.
- **M3** — Govern cross-language snapshot validation with independent validators and shared
  fixtures.
- **M4** — Sanitize upstream HTML with a proven browser-side allowlist
  sanitizer.
- **M5** — Skip overlapping backend synchronization ticks.

The following resolved interpretations are used in the matrix:

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

| SEED section | In-scope requirement or significance | Mapping |
| --- | --- | --- |
| `Vision` | Read-only anonymous PWA; frozen backend-observed snapshots; personal-grade simplicity; no write, identity, personalization, search, bookmark, recommendation, or client-fetch expansion. | US-008, US-012, US-014; `docs/TODO.md` scope guardrail; no MADR. |
| `Personas / Reader` | Anonymous cached-snapshot browsing. | US-008–US-015. |
| `Personas / Operator` | Deploy, monitor, and upgrade. | US-001, US-016–US-018. |
| `Axioms` | Immutable independent lineages; one complete local lineage; backend authority; exact upstream order/HTML; text/media separation; visible degradation; ephemeral Memcached; browser serving; Preact/browser APIs; trace-first operation; operational state only. | M1–M5 where a mechanism remains open; US-002–US-015 and US-018; `docs/TODO.md` scope guardrail; I2–I8. |
| `Full Requirements / Product` | Read-only, anonymous, no product extras, original canonical URLs, exact ordering. | US-008, US-012–US-014; `docs/TODO.md` scope guardrail. |
| `Full Requirements / Snapshot model` | Fresh immutable lineage, no prior-cache influence, old active until atomic replacement, no partial visibility, backend authority. | M1–M3; US-003–US-007, US-010; I2 and I7. |
| `Full Requirements / Backend cache` | Every observed board; first 250 catalog threads; first 250 posts; oversize marker; no binaries; values unchanged except metadata. | US-002–US-005; M1 and M3. |
| `Full Requirements / Client synchronization` | Approximately hourly stable 5–60-second installation jitter; complete staging and atomic activation; old-lineage continuity; one active lineage; transparent transport. | US-009–US-011; M2, M3; I1 and I2. |
| `Full Requirements / Local storage` | IndexedDB mandatory/exclusive for snapshots; Cache Storage shell-only; clear failure; total local reset; quota preserves current lineage. | US-008–US-010; M2. |
| `Full Requirements / Rendering` | Backend retains HTML; frontend sanitizes; unsupported markup becomes text; external/canonical quote links; no raw injection. | M4; US-013–US-014; I6. |
| `Full Requirements / Media` | Online thumbnail auto-load; explicit full media; ordinary browser cache only; placeholder/manual retry; spoilers hidden. | US-015. |
| `Full Requirements / User interface` | Mobile-first responsive compact catalogs; nested/collapsible replies; always-visible lineage ID/age; visible failed/oversize states. | US-012, US-014; I3. |
| `Full Requirements / Upstream acquisition` | Configurable four-hour backend schedule; stable 5–60-second startup jitter; concurrency 10; global rate limit; five-second timeout; configurable four-hour lineage deadline; transient-only bounded retry; exact User-Agent. | US-001, US-003, US-004, US-007; M5. |
| `Full Requirements / Failure handling` | Failed known resources; absent unknown resources; degraded activation; pre-activation failure preserves old lineage; prominent degraded telemetry; all unfinished resources fail at deadline. | US-003–US-007, US-010, US-018; I4, I5, I7. |
| `Full Requirements / Deployment` | Compose; sole loopback edge; exact routing; separate internal services; ingress TLS; project-image hardening; `FOURVISOR_`; shallow health. | US-001, US-016, US-017. |
| `Full Requirements / Platform and testing` | Linux amd64; Chrome Android 150+; unit/integration only. | US-008, US-016; every story's validation and `docs/TODO.md` validation assumptions. |
| `Full Requirements / Observability` | Go OpenTelemetry boundary; sparse metrics/logs; request/sync roots and required children; successful sampling; failed retention. | US-001, US-003–US-007, US-018; I8. |

### High-level architecture

| SEED section | In-scope requirement or significance | Mapping |
| --- | --- | --- |
| `High-Level Architecture` | Client-first topology; only edge is browser-facing; client never fetches textual resources individually; backend builds and serves one lineage. | US-006, US-008–US-010, US-012, US-015, US-017. |
| `Client architecture` | Mandatory IndexedDB; immediate local render; due-only refresh; inactive stage; validate then atomic swap; at most active plus incoming; quota continuity. | M2, M3; US-009–US-011. |
| `Backend` | One process/cache/schedule; one active/building lineage; write-before-pointer; immediate eviction; twice-interval TTL; missing block is `410`. | M1, M5; US-005–US-007. |
| `Single lineage authority` | Client accepts and never reconciles backend-selected lineage. | US-010; I2. |
| `Snapshot contents` | Metadata, complete bounded text hierarchy, explicit failed/oversize resources, unchanged HTML/media references; no binaries or user/session state. | M3; US-002–US-004, US-015; `docs/TODO.md` scope guardrail. |
| `HTTP routing` | Exact edge order and `/api` stripping; only snapshot/health; status contracts; secret-free health body; no readiness route. | US-001, US-006, US-017. |
| `Snapshot transfer` | One logical JSON response; internal Memcached blocks; ingress Brotli; no range/resume/resource/manifest/binary protocol. | M1; US-006, US-010, US-017; I1. |
| `Snapshot schema version 1` | Exact strict wrappers, types, states, cardinalities, ULID/UTC rules, opaque upstream objects, v1-only rejection. | M3; US-002, US-005, US-010. |
| `Upstream acquisition` | Scheduled-only bounded acquisition; exact defaults and fidelity; no repair/inference. | US-003, US-004, US-007; M5. |
| `Media path` | Browser-to-4chan only; automatic thumbnails; explicit full media/spoiler reveal; ordinary cache; placeholder/manual retry. | US-015. |
| `Observability path` | Required trace roots/children, minimal metric families, Collector tail policy. | US-001, US-003–US-007, US-018. |

### Operational flows

| SEED section | In-scope requirement or significance | Mapping |
| --- | --- | --- |
| `Client startup` | IndexedDB required; blocking clear failure; immediate active render; explicit empty state; schedule next sync. | US-009, US-011. |
| `Client synchronization` | Due-only complete request; inactive staging; validation; atomic activation/cleanup; classified visible errors; next scheduled retry; backend authority. | M2, M3; US-010, US-011; I2. |
| `First installation jitter` | Random non-fingerprint seed in IndexedDB; never transmitted; stable until reset; 5–60-second offset. | US-009, US-011. |
| `Scheduled backend synchronization` | Four-hour stable instance jitter; new ULID; hard deadline; ordered acquisition; publication/activation/eviction; one build at a time. | M5; US-007, relying on US-003–US-005. |
| `Board acquisition` | Exact board order/data; transient-only retry; root failure activates `boards.failed`; no prior/inferred boards. | US-003, US-007. |
| `Catalog acquisition` | Every known board; first 250 across preserved pages; failed versus absent semantics. | US-003; I4. |
| `Thread acquisition` | Fetch each selected thread; 250 cap; oversize; retry classification; deadline failure; no remainder endpoint. | US-004. |
| `Retry behavior` | Only network/timeout/rate limiting; `Retry-After`; global limiter; per-request/deadline bounds; no repair queue. | US-003, US-004. |
| `Lineage construction and activation` | Complete blocks/metadata before pointer; failure preserves old active; immediate eviction; twice-interval TTL. | M1; US-005, US-007; I7. |
| `Serving a snapshot` | Pointer/metadata/all blocks required before `200`; otherwise `410`; one logical response. | M1; US-006. |
| `Degraded lineage completion` | Always activate resource-degraded valid lineages; tolerance only marks root error/log; include lineage/failure/tolerance attributes. | US-007, US-018; I5. |
| `Client rendering` | Active local lineage only; no fetch/reconcile/inference/reorder/filter; visible failed/oversize; lineage ID/age. | US-012, US-014; I3. |
| `Missing local resource` | Explicit unavailable message; zero backend fetch; canonical external option. | US-012, US-014. |
| `Post markup rendering` | Isolated parse/strict allowlist; unsupported markup as text; original safe links; canonical quote links. | M4; US-013; I6. |
| `Thumbnail loading` | Direct online request; placeholder on offline/error; unlimited manual retry; no explicit cache. | US-015. |
| `Full media loading` | Explicit action; original media/native behavior; placeholder/manual retry; no proxy/transform/persist; spoiler reveal. | US-015. |
| `Local reset` | Confirm; delete IndexedDB/app caches/seed/incoming; reload; local only. | US-009. |
| `Backend component failure` | Required dependency fails operation; no fallback caches/stores/coordination; local lineage is continuity. | US-001, US-005, US-006, US-010, US-017; I8 for optional telemetry. |
| `Health check` | Backend response, Memcached reachability, DNS only; no deep freshness/upstream-quality probe. | US-001, US-017. |
| `Trace flow for inbound requests` | Request root; pointer/metadata/block/serialization children; error logs/status propagation; no routine logs. | US-001, US-006, US-018. |
| `Trace flow for scheduled synchronization` | Sync root with lineage ID; board/catalog/thread/cache/activation/eviction children; completion event. | US-003–US-007, US-018. |
| `Telemetry export` | Enumerated meaningful logs; minimal labels; no lineage/thread/URL/key/client/raw-error labels. | US-003–US-007, US-018. |

### Deployment, rationale, and cross-cutting semantics

| SEED section | In-scope requirement or significance | Mapping |
| --- | --- | --- |
| `Deployment View` | Five internal Compose components behind one ingress-to-loopback edge; backend-to-upstream and Collector export paths. | US-016–US-018. |
| `Deployment philosophy` | Minimal single-node topology and client continuity. | US-017. |
| `Deployment View / Backend` | Exactly one backend-owned Memcached instance. | US-005, US-017. |
| `Container model` | Only edge host bind; frontend/backend/cache/Collector internal; first-party hardening only; native third-party config; `FOURVISOR_` only for Go. | US-001, US-016, US-017. |
| `Health model` | Shallow three-part backend health. | US-001, US-017. |
| `Traffic` | Exact ingress/edge/internal flow; backend text only; browser media direct. | US-006, US-015, US-017. |
| `Scheduling` | Stable startup jitter followed by configured interval. | US-007. |
| `Failure model` | Required components fail dependents; telemetry remains optional; specified single-service outcomes. | US-001, US-005–US-007, US-010, US-017, US-018; I8. |
| `Security` | Ingress TLS; loopback edge; no internal exposure; private Memcached; first-party hardening; no enterprise claims. | US-016, US-017. |
| `Deployment View / Observability` | Backend OTLP to Collector; third-party/Caddy stdout excluded; Collector sampling/export. | US-018. |
| `Design Notes / Snapshot-first architecture` | Complete local snapshots, never intermediate. | M1–M3; US-005, US-010. |
| `Design Notes / Client-first design` | IndexedDB is post-sync serving layer; offline text continuity. | M2; US-009, US-010, US-012, US-014. |
| `Design Notes / Immutable lineages` | From scratch; no historical eligibility; complete-before-active. | US-003–US-007. |
| `Design Notes / No incremental synchronization` | Complete replacement; no merge/differential logic. | US-010; I1. |
| `Design Notes / Single backend` | No redundancy/coordination; client continuity. | US-005, US-007, US-017. |
| `Design Notes / Memcached as a serving cache` | Pointer-selected ephemeral data; immediate deletion; TTL fallback; no durability. | M1; US-005, US-006. |
| `Design Notes / Upstream fidelity` | Preserve ordering/semantics/HTML; add only cache metadata. | M3; US-002–US-004, US-012–US-014. |
| `Design Notes / Binary exclusion` | Text only; browser ordinary media cache. | US-004, US-015. |
| `Design Notes / Honest degradation` | Failed/oversize visible and degraded lineage telemetry. | US-003, US-004, US-007, US-012, US-014. |
| `Design Notes / Browser platform first` | Preact only; direct browser APIs; no broad state/routing abstractions. | US-008–US-015. |
| `Design Notes / Trace-first observability` | Root/child traces, sparse meaningful logs. | US-001, US-003–US-007, US-018. |
| `Design Notes / Simplicity over flexibility` | No coordination, repair, warming, resume, personalization, or mutation. | M1–M5 choices; `docs/TODO.md` scope guardrail. |

### Detailed observability and failure semantics

| SEED section | In-scope requirement or significance | Mapping |
| --- | --- | --- |
| `Detailed Observability / Philosophy` | Detailed traces, few metrics, sparse logs; each signal operationally useful. | US-001, US-018. |
| `OpenTelemetry` | Only Go observability framework; backend OTLP to Collector; Collector receives/samples/exports. | US-001, US-018. |
| `Tracing` | Every request/sync root; HTTP/cache/lifecycle/serialization/validation children; useful attributes; no high-cardinality metric labels. | US-001, US-003–US-007, US-018. |
| `Metrics / HTTP` | Server/client request counts and latency. | US-001, US-003, US-018. |
| `Metrics / Cache` | Operations, hits, misses, errors, latency. | US-005, US-006, US-018. |
| `Metrics / Lineages` | Duration, success/degraded outcomes, failed resources, active age. | US-007, US-018. |
| `Logging` | Enumerated state/error events; exclude routine request/cache/outbound success chatter. | US-001, US-003–US-007, US-018. |
| `Error handling` | Log, fail relevant spans/parents; excessive degradation attributes/log/root error. | US-001, US-003–US-007, US-018. |
| `Sampling` | Collector retains all error traces and ~10% successful; no app sampling. | US-018. |
| `Detailed Observability / Deployment` | Direct OTLP; no local buffering/secondary stack. | US-018. |
| `Design principles` | Trace answers why, metrics health, logs event; emit less. | US-018 acceptance/documentation. |
| `Failure Semantics / Philosophy` | No failover/repair/reconstruction; dependent operation fails; local lineage continuity. | US-005–US-007, US-010, US-017; `docs/TODO.md` scope guardrail. |
| `Backend component failures` | Exact outage effects for edge/frontend/backend/cache/upstream/Collector/ingress. | US-001, US-005–US-007, US-010, US-017, US-018; I8. |
| `Client failures` | IndexedDB unavailable/corrupt; quota; network/backend; explicit schema mismatch. | US-009, US-010. |
| `Synchronization failures` | Never activate partial; retain old; retry next scheduled interval. | US-010, US-011. |
| `Lineage degradation` | Present/failed/oversize/absent; always activate valid degraded lineage; threshold changes telemetry only. | US-002–US-004, US-007; I4/I5. |
| `Cache failures` | Missing active block set returns `410 Gone`. | US-006. |
| `Media failures` | Placeholder/manual retry; textual availability unaffected; no automatic retry. | US-015. |
| `Upstream failures` | Bounded transient retry; permanent/exhausted/unfinished become failed until next lineage. | US-003, US-004, US-007. |
| `Failure matrix` | Each listed visible outage outcome. | US-001, US-006, US-007, US-009, US-010, US-014, US-015, US-017. |
| `Failure Semantics / Summary` | Client/backend outcomes for sync, upstream, storage, backend, and media failure. | US-007, US-010, US-011, US-015. |
| `Operational principle` | Fail fast/visibly; preserve last complete client snapshot when possible. | US-001, US-005–US-007, US-009–US-011, US-015. |

### Technology, locked decisions, and exclusions

| SEED section | In-scope requirement or significance | Mapping |
| --- | --- | --- |
| `Technology Stack / Backend` | Go, `net/http`, Memcached, OpenTelemetry SDK/OTLP. | US-001, US-003–US-007, US-018. |
| `Frontend` | Preact, Tailwind, TypeScript, ES modules, Vite/Vitest, direct IndexedDB/SW/Cache/Fetch. | US-002, US-008–US-015. |
| `Data formats` | JSON, ingress Brotli, unchanged HTML, ULID. | US-002, US-006, US-007, US-013, US-017. |
| `Infrastructure` | Docker/Compose/Caddy and first-party distroless/rootless/read-only. | US-016, US-017. |
| `Networking` | HTTPS ingress; ingress Brotli; loopback edge; prefix stripping; internal services; upstream HTTP. | US-003, US-006, US-015, US-017. |
| `Observability` | OTel/OTLP, Collector tail sampling, structured logs, metrics, traces. | US-001, US-018. |
| `Testing` | Vitest and Go unit/integration only. | Every story validation; `docs/TODO.md` validation assumptions. |
| `Browser platform` | PWA/manifest/IndexedDB/SW/Cache Storage; History API not for app routing. | US-008–US-015. |
| `Operating systems` | Linux amd64 and Chrome Android 150+. | US-008, US-016. |
| `Configuration` | Go config only from `FOURVISOR_`. | US-001 and each backend story's documented configuration. |
| `Deliberate exclusions` | No alternate frameworks, databases, orchestration, queues, distributed cache, SSR. | Global guardrail and individual out-of-scope sections. |
| `Technology Rationale / Philosophy` | Conservative, lightweight, personal-grade technology selection. | All final stories and `docs/TODO.md` personal-project scope assumptions; no implementing MADR required. |
| `Go` | Small stdlib HTTP backend without framework. | US-001, US-003–US-007. |
| `Memcached` | Disposable single serving cache, pointer namespacing, scheduled reconstruction. | M1; US-005–US-007. |
| `Preact` | Sole narrow rendering abstraction; browser APIs primary. | US-008, US-012–US-015. |
| `Tailwind CSS` | Vite-integrated styling. | US-008, US-012, US-014. |
| `Vite` | Frontend build/dev tooling only. | US-008, US-016. |
| `Vitest` | Frontend unit/integration validation only. | US-002, US-008–US-015. |
| `IndexedDB` | Active lineage persistence and offline capability; exactly one active after sync. | M2; US-009–US-011. |
| `Service Worker` | Shell only, offline startup, no snapshot Cache Storage. | US-008–US-010. |
| `Docker Compose` | Minimal reproducible deployment, no Kubernetes. | US-017. |
| `First-party container hardening` | Project-built images distroless/rootless/read-only; do not rebuild third-party images just to harden. | US-016, US-017. |
| `OpenTelemetry` | Unified vendor-neutral Go signals, trace-first. | US-001, US-018. |
| `Brotli-compressed JSON` | Ingress-supplied standard HTTP compression; no custom serialization. | US-006, US-017; I1. |
| `Technology Rationale / Deliberate omissions` | Avoid systems solving excluded distributed/product concerns. | Global guardrail. |
| `Locked Decisions / Product` | All fixed product identity and exclusions. | US-008, US-012–US-015; `docs/TODO.md` scope guardrail. |
| `Locked Decisions / Snapshot model` | Exact v1, no compatibility, complete atomic backend-authoritative lineage. | M1–M3 mechanisms; US-002, US-005–US-007, US-010. |
| `Locked Decisions / Backend cache` | Exact board/thread/post limits, oversize, no binary, unchanged HTML. | US-002–US-004. |
| `Locked Decisions / Frontend` | Exact stack/target/storage/UI; no state framework/router. | US-008–US-015. |
| `Locked Decisions / Rendering` | Sanitize, text fallback, clickable/canonical links, visible degradation. | M4; US-012–US-014; I6. |
| `Locked Decisions / Synchronization` | Default approximately-hourly client jitter and four-hour backend jitter; complete swap; one retained active lineage. | M2, M5; US-005, US-007, US-009–US-011. |
| `Locked Decisions / Backend` | Exact backend/cache/topology/routing/compression/hardening/platform/configuration. | M1; US-001, US-005–US-007, US-016, US-017. |
| `Locked Decisions / Testing` | Unit/integration only. | Global principles and every validation section. |
| `Locked Decisions / Upstream` | Limiting/concurrency/timeout/deadline/retry/User-Agent. | US-003, US-004, US-007. |
| `Locked Decisions / Observability` | Go-only OTel contract; trace first; sparse metrics/logs; Collector tail retention. | US-001, US-018. |
| `Locked Decisions / Failure semantics` | Required operation fails; resource-degraded activation; total outage; old lineage preservation; visible degradation. | US-001, US-003–US-007, US-010, US-018; I4/I5/I7/I8. |
| `Locked Decisions / Out of scope` | No enterprise/multi-browser/arm64/distributed/incremental/client-fetch/media-cache/database/workflow/queue/Kubernetes work. | Global guardrail and story out-of-scope sections. |
| `Out of Scope / User interaction` | No social/write/account/auth features. | Global guardrail; US-012/US-014 negative criteria. |
| `Out of Scope / Personalization` | No preferences/settings/read/bookmark/search/recommendation/feed. | Global guardrail; US-011, US-012, US-014. |
| `Out of Scope / Snapshot behavior` | No incremental/partial/merge/differential/client fetch/history/version reconcile. | US-006, US-010, US-014; `docs/TODO.md` scope guardrail; I1/I2. |
| `Out of Scope / Backend architecture` | No replicas/shared cache/coordination/workflow/repair/replay/guarantees. | M5; US-005, US-007, US-017; `docs/TODO.md` scope guardrail. |
| `Out of Scope / Data storage` | No durable backend data/media/index stores or proxying. | US-005, US-015; `docs/TODO.md` scope guardrail. |
| `Out of Scope / Media` | No server/offline media cache, automatic retry, transcoding/optimization/streaming infrastructure. | US-015. |
| `Out of Scope / Frontend` | No alternate framework/browser/router/SSR/MPA. | US-008, US-012; `docs/TODO.md` scope guardrail. |
| `Out of Scope / Testing` | No smoke/E2E/deployment tests. | Global principles and all validation sections. |
| `Out of Scope / Deployment` | No arm64/enterprise/Kubernetes/mesh/autoscaling/replication/consensus/stateful orchestration/complex probes. | US-016, US-017; `docs/TODO.md` scope guardrail. |
| `Out of Scope / Observability` | No verbose/high-cardinality/audit/analytics/tracking/replay. | US-001, US-003–US-007, US-018. |
| `Out of Scope / Product philosophy` | Snapshot reader only, not social/archive/search/cache/distributed showcase. | Global guardrail; entire story set. |



## Resolved Planning Assumptions

- Version 1 initially transfers one logical JSON response. Public fixed batches remain a future option only if measured size requires them.
- Browser storage uses distinct local generation keys and fixed-size serialized records; upstream `lineageId` is never the local storage identity.
- The client accepts a backend-authoritative refresh even when its lineage ID or timestamp matches or predates local metadata.
- Reply nesting is presentation-only and never changes upstream post order.
- Known attempted resources with classified failures are `failed`; descendants never established by a parent response remain absent.
- Degradation tolerance affects telemetry severity only and never gates activation.
- Unsafe URL schemes become non-clickable text; link fidelity never overrides the HTML trust boundary.
- The active-pointer switch is the commit boundary. Pre-switch failures preserve the old lineage; post-switch cleanup failures preserve the new lineage and use TTL cleanup.
- Collector/exporter failure does not fail unrelated application operations.
- The global upstream default is one request per second, matching the official 4chan API rule. Transient requests receive at most two retries with one- then two-second backoff unless `Retry-After` is longer.
- The default excessive-degradation tolerance is 10 failed resources. The default synchronization interval is four hours, configurable through `FOURVISOR_`; Memcached lineage TTL is always twice the configured interval.
- Every project-controlled local or Compose service listener, proxy target, container, health-check, and published port uses 65100–65199; remote upstream, ingress, and operator exporter destinations are excluded. Only edge Caddy publishes a host port, explicitly on `127.0.0.1`.
- Semantic and keyboard-operable controls are implementation-quality requirements, not new SEED product scope.

## Coverage Result

- All in-scope `docs/SEED.md` sections map to at least one MADR, User Story, or explicit assumption.
- Locked decisions and axioms are not duplicated as MADRs.
- Every story dependency points to an earlier story; only direct prerequisites are listed.
- Testing remains limited to unit and integration tests.
- No requirement remains unmapped or uncertain after synthesis.
