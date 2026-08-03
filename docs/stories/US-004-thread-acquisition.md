# US-004: Complete lineages with bounded thread acquisition

## Goal

Expand each selected catalog entry into a bounded, immutable textual thread resource without changing upstream order or hiding incomplete results.

## User Value

The Reader gets the first 250 posts exactly as observed, with oversized, failed, and deadline-expired threads still visible and understandable.

## Scope

- Fetch the thread belonging to each of the first 250 catalog summaries through the shared outbound limiter and retry policy.
- Preserve every returned post object, original post HTML, media reference, and post order unchanged.
- Store zero through 250 posts as `present`; truncate more than 250 to the first 250 and mark the resource `oversize`.
- Mark terminal/exhausted failures and unfinished resources at the configured lineage deadline as `failed` without a failure-detail payload.
- Stop scheduling or continuing unfinished acquisition after deadline/cancellation and propagate cancellation through workers.
- Record oversize detection and errors as meaningful logs and child spans; expose failed-resource counts without high-cardinality labels.

## Out of Scope

- Retrieval of posts beyond 250, on-demand fetches, media download/proxy/cache, Memcached publication, or retry queues/background repair.

## Dependencies

- US-003.

## Related MADRs

- None. Worker-pool mechanics are a local Go implementation detail under the locked global limits.

## Traceability

- `Full Requirements / Backend cache` and `/ Failure handling` (`docs/SEED.md:108-116`, `180-190`): 250-post cap, oversize marking, unchanged textual resources, failed resources, and deadline behavior.
- `High-Level Architecture / Snapshot contents` and `/ Upstream acquisition` (`docs/SEED.md:348-374`, `496-528`): required thread resources, unchanged HTML/media references, bounded acquisition, and no binary caching.
- `Operational Flows / Thread acquisition` and `/ Retry behavior` (`docs/SEED.md:753-811`): present/oversize behavior, terminal failures, retries, and deadline.
- `Failure Semantics / Upstream failures` (`docs/SEED.md:1724-1734`): transient retry and unfinished-resource failure.
- `Locked Decisions / Backend cache` and `/ Upstream` (`docs/SEED.md:2126-2134`, `2197-2205`): fixed caps, unchanged HTML, binary exclusion, global limiting, and time bounds.

## Acceptance Criteria

1. Every eligible catalog summary retains its original position and has a thread resource unless the upstream thread is genuinely unobserved/absent.
2. Responses of 0–250 posts produce `present` with the same ordered opaque posts; responses over 250 produce `oversize` with exactly the first 250.
3. No backend path retrieves or exposes post 251 or any media binary.
4. Terminal/exhausted requests and requests unfinished at the lineage deadline become exact failed wrappers; no work continues after cancellation/deadline.
5. A changed, missing, or newly oversized thread is evaluated only from the current observation, never copied from a prior lineage.
6. Oversize and failure telemetry is meaningful, cause-preserving, and free of raw post content or high-cardinality metric labels.

## Validation

- Integration-test 0, 250, and 251-post responses; transient/permanent errors; a deadline with queued and in-flight requests; and context cancellation.
- Unit-test truncation, order/opaque-value preservation, failed-resource counting, and absence distinction.
