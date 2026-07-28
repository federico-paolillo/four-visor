# US-003: Observe every board and its first 250 catalog threads

## Goal

Construct the board and catalog portion of a fresh lineage from scheduled 4chan observations while preserving upstream fidelity and bounded outbound behavior.

## User Value

The Reader can eventually see every observed board and the first 250 catalog entries in exactly the order and page boundaries returned by 4chan, including honest failure states.

## Scope

- Implement the shared 4chan HTTP acquisition boundary with a default global limit of one request per second, default maximum concurrency of 10, five-second request timeout, at most two retries after the initial attempt with one- then two-second backoff, context cancellation, and the required deployed-commit User-Agent.
- Fetch the board list and, when present, every observed board's catalog.
- Preserve board order, upstream values, catalog page order/boundaries, every page metadata field other than `threads`, and the first 250 thread summaries across pages.
- Represent a technical or lineage-deadline board-list failure as `boards.state = failed`; represent a technical catalog failure as `catalog.state = failed`; mark every known catalog unfinished at the lineage deadline as failed; leave only genuinely unexplained missing resources absent. External or shutdown cancellation still aborts construction rather than becoming resource degradation.
- Start each construction with no resource reuse from a prior lineage.
- Instrument outbound requests as child spans and emit low-cardinality client request metrics and error logs without logging raw URLs, identifiers as metric labels, response bodies, or secrets.
- Document acquisition configuration and failure classification.
- Add and validate the owning `FOURVISOR_` rate, concurrency, timeout, retry, and backoff settings with the defaults above.

## Out of Scope

- Fetching thread bodies, Memcached publication, activation, the scheduler, background repair, automatic replay, or inferring why upstream content is absent.

## Dependencies

- US-001.
- US-002.

## Related MADRs

- None. Rate values, bounded retry/backoff, endpoint adapter boundaries, and opaque-object mechanics are story-level implementation/configuration details under locked acquisition semantics.

## Traceability

- `Full Requirements / Backend cache` and `/ Upstream acquisition` (`docs/SEED.md:108-116`, `168-179`): every board, first 250 catalog threads, global rate limit, concurrency 10, timeout, deadline-aware retries, and User-Agent.
- `High-Level Architecture / Upstream acquisition` (`docs/SEED.md:496-528`): scheduled-only acquisition policy, transient retries, fidelity, and no repair/inference.
- `Operational Flows / Board acquisition`, `/ Catalog acquisition`, and `/ Retry behavior` (`docs/SEED.md:707-732`, `733-752`, `780-811`): exact ordering, failed versus absent, Retry-After, and shared limits.
- `Design Notes / Immutable lineages` and `/ Upstream fidelity` (`docs/SEED.md:1334-1346`, `1374-1383`): from-scratch construction and unchanged upstream semantics.
- `Detailed Observability / Tracing`, `/ Metrics`, and `/ Logging` (`docs/SEED.md:1481-1587`): outbound spans, client metrics, sparse logs, and low-cardinality signals.

## Acceptance Criteria

1. A successful observation contains every board in upstream order and at most the first 250 thread summaries per catalog while preserving page order, page boundaries, metadata, and opaque values.
2. Every request attempt, including retries, passes through one process-wide one-request-per-second limiter by default, never exceeds 10 concurrent outbound requests, times out after five seconds, carries `User-Agent: 4Visor/<deployed-commit-hash>`, and stops promptly on lineage cancellation.
3. Only network errors, timeouts, and rate limiting are retryable; at most two retries follow the initial attempt by default with one- then two-second backoff, `Retry-After` takes precedence when present, and no attempt crosses the lineage deadline.
4. Board-list technical failure or unfinished board acquisition at the lineage deadline yields the exact `boards.state = failed` wrapper; external/shutdown cancellation aborts construction and preserves the active lineage.
5. Catalog technical failure remains visible on its board, and every known catalog unfinished when the lineage deadline expires receives the exact failed wrapper; descendants never established by a parent response remain absent.
6. Prior-lineage data cannot enter a new construction when an upstream resource disappears or changes.
7. Outbound failures preserve their cause for spans/logs while diagnostics remain secret-free and metric labels remain low cardinality.

## Validation

- Integration-test against a controllable HTTP server that returns ordered multi-page catalogs, rate limiting with `Retry-After`, permanent failures, slow responses, deadline-expired board/catalog requests, external cancellation, and disconnects.
- Unit-test first-250 selection across page boundaries, failure classification, concurrency, cancellation, User-Agent generation, and absence semantics.
