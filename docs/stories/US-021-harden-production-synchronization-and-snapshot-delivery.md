# US-021: Harden production synchronization and snapshot delivery

## Goal

Keep production synchronization telemetry complete and make large snapshot delivery reliable and diagnosable.

## User Value

The Operator can inspect one complete synchronization trace and serve a large active snapshot without an opaque proxy failure.

## Scope

- Retain every complete synchronization trace while preserving error retention and 10% successful-trace sampling for other traffic.
- Keep the tail-sampling decision open for the configured four-hour acquisition plus publication.
- Remove the backend's absolute response-write deadline and explicitly stream backend proxy responses without adding arbitrary proxy limits.
- Demote expected oversized-thread records to debug.
- Warn once for each terminal per-resource fetch failure after retries, in addition to bounded aggregate logs and metrics.
- Raise Memcached capacity to 2048 MiB for overlapping approximately 800 MiB lineages.
- Use repository-edge Brotli only when the pinned Caddy build supports it; otherwise keep compression at the VPS Caddy.

## Out of Scope

- Application-side trace sampling, public snapshot blocks, resumable downloads, new compression modules or images, retry-attempt log spam, raw resource identifiers, URLs, bodies, cache values, or errors.

## Dependencies

- US-020.

## Related MADRs

- None. This story corrects production limits without changing snapshot or lineage semantics.

## Traceability

- `High-Level Architecture / Snapshot transfer` and `Operational Flows / Serving a snapshot`: one validated logical response and Brotli at the VPS ingress.
- `Operational Flows / Trace flow for scheduled synchronization` and `Detailed Observability`: one root with complete children and tail-based retention.
- `Deployment View / Container model`: stock third-party images and one loopback-only published edge.

## Acceptance Criteria

1. A synchronization lasting longer than the former 40-minute decision wait is exported as one complete trace, successful or failed; other failed traces remain retained and other successful traces remain sampled at 10%.
2. A large snapshot is not terminated by a backend write deadline, and the repository edge streams it without an added response timeout.
3. Snapshot validation and cache failures still produce their controlled HTTP response before success is committed.
4. Oversized threads emit debug rather than warning records.
5. Every actual terminal boards, catalog, or thread fetch failure emits exactly one warning with only lineage ID and bounded failure dimensions; aggregate logs and metrics remain, and undispatched work emits no fetch warning.
6. Development and production Compose configurations give Memcached 2048 MiB while retaining disabled evictions and private networking.
7. The pinned stock Caddy module inventory decides Brotli support. Without a Brotli encoder, the repository edge keeps upstream content uncompressed and the VPS Caddy applies Brotli once.

## Validation

- Run `mise run be:build`, `mise run be:test`, and `mise run be:lint`.
- Unit-test per-resource warning cardinality, controlled fields, secret exclusion, OTLP filtering, and debug oversized-thread severity.
- Render both Compose configurations and validate the pinned Caddy and Collector configurations.
- Inspect the pinned Caddy module inventory for Brotli; do not run smoke, end-to-end, or deployment tests.
