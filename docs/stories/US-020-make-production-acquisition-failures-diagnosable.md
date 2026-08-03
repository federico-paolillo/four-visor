# US-020: Make production acquisition failures diagnosable

## Goal

Explain failed acquisition, publication, and snapshot-serving work from bounded telemetry without exposing upstream or cache values.

## User Value

The Operator can correlate a degraded lineage by ULID, distinguish where it failed, and estimate whether the thread queue can finish before the lineage deadline.

## Scope

- Aggregate terminal acquisition failures once per lineage and bounded combination of resource, stage, controlled type/cause, HTTP status, retry attempt, and exhaustion.
- Carry the lineage ULID through acquisition logs and spans without using it as a metric attribute.
- Distinguish queue, rate, concurrency, request, body, decode, and retry failures and warn when queued thread work exceeds the remaining global-rate capacity.
- Emit one value-free effective-policy record at application composition.
- Classify publication failures by controlled stage, error type, and detail.
- Identify snapshot pointer, completion, block, serialization, and response failures with the resulting status; add lineage correlation only after pointer validation.
- Export `lineage.resource.failure.count` and the last committed serialized size as `lineage.active.size` while preserving `lineage.failed_resource.count` semantics.
- Extend the OTLP allowlist only for the controlled messages and attributes introduced here.

## Out of Scope

- Per-resource identifiers, raw URLs, response bodies, cache keys, transport/cache error strings, retry queues, background repair, high-cardinality metrics, or a second logging framework.

## Dependencies

- US-018 and US-019.

## Related MADRs

- None. This story refines the existing trace-first observability contract without changing acquisition or activation semantics.

## Traceability

- `High-Level Architecture / Upstream acquisition` and `Operational Flows / Retry behavior`: bounded global rate, retries, and the configured lineage deadline.
- `Operational Flows / Lineage construction and activation`: atomic pointer replacement and post-activation cleanup.
- `Operational Flows / Serving a snapshot`: pointer, completion, block, and serialization failure boundaries.
- `Detailed Observability`: sparse logs, low-cardinality metrics, value-free errors, and trace-first diagnosis.

## Acceptance Criteria

1. Equivalent terminal resource failures produce one lineage summary whose count and dimensions explain the resource, stage, controlled cause, status, attempt, and exhaustion without disclosing upstream values.
2. Queue, rate, concurrency, request, body, decode, and retry deadline paths remain distinguishable, and an impossible thread queue emits one rate-capacity warning.
3. One startup lifecycle record reports only the effective acquisition and synchronization policy.
4. Publication logs and spans identify a bounded stage and snapshot-specific validation classification with controlled detail.
5. Snapshot failures report component and response status, omitting lineage ID until a valid pointer has been read.
6. `lineage.resource.failure.count` has only bounded failure dimensions; `lineage.failed_resource.count` remains the total failed wrappers per activated lineage.
7. `lineage.active.size` is an attribute-free synchronous `Int64Gauge` in `By`, changes only after committed activation, retains its prior value after failed publication, and updates before cleanup.
8. OTLP filtering retains the new operational records and fields while local stderr remains unchanged.

## Validation

- Run `mise run be:build`, `mise run be:test`, and `mise run be:lint`.
- Unit-test representative failure dimensions, aggregation counts, rate capacity, publication/snapshot fields, value exclusion, OTLP filtering, and both new metric instruments.
