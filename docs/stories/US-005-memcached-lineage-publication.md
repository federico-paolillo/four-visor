# US-005: Publish one immutable lineage atomically through Memcached

## Goal

Make a completed logical lineage available through an ephemeral Memcached namespace without exposing partial publication or sacrificing the prior active lineage on failure.

## User Value

The Reader sees either the last complete server snapshot or the next complete one, never a partially written mixture.

## Scope

- Split a validated lineage into deterministic Memcached blocks below item limits, with lineage-scoped immutable keys, completion metadata, and one active-lineage pointer.
- Write and verify all required blocks and completion metadata before changing the active pointer.
- Preserve the old active pointer on construction validation, cache write, publication, or cancellation failure.
- After successful activation, evict the previous lineage immediately and assign all lineage keys a TTL calculated from twice the synchronization interval supplied by the scheduler as cleanup insurance.
- If post-activation eviction fails, keep the new lineage active, report the cleanup error, and rely on TTL rather than rolling the active pointer back.
- Treat Memcached as disposable serving state with no durable fallback.
- Trace cache operations and lineage validation/activation/eviction, update cache metrics, and log only lifecycle transitions, cleanup, and errors.
- Document key lifecycle and cache-loss recovery semantics without disclosing actual keys in telemetry.

## Out of Scope

- Durable storage, cache replication, distributed locking, multiple active lineages, historical browsing, HTTP snapshot serving, or fallback stores.

## Dependencies

- US-004.

## Related MADRs

- [MADR 0001](../madrs/0001-backend-lineage-blocks.md), “Store backend lineages as ordered fixed-size serialized blocks.” Exact block byte size, key spelling, and cache-operation mechanics remain story-level details.

## Traceability

- `Full Requirements / Snapshot model` and `/ Failure handling` (`docs/SEED.md:98-107`, `180-190`): immutable independent lineages, atomic activation, and prior-lineage preservation.
- `High-Level Architecture / Backend` (`docs/SEED.md:306-342`): one active/building lineage, block-before-pointer order, immediate eviction, twice-interval TTL, and ephemeral cache behavior.
- `Operational Flows / Lineage construction and activation` (`docs/SEED.md:812-839`): all writes and completion metadata precede pointer switch; listed failures preserve active lineage.
- `Design Notes / Memcached as a serving cache` (`docs/SEED.md:1363-1373`): active pointer, immediate removal, and TTL as fallback only.
- `Detailed Observability / Tracing`, `/ Cache metrics`, and `/ Logging` (`docs/SEED.md:1481-1587`): cache/lifecycle spans, cache signals, and lifecycle-only logs.

## Acceptance Criteria

1. Before activation, no reader of the active pointer can resolve the building lineage; after activation, all required blocks and completion metadata are resolvable.
2. Any injected validation, write, metadata, pointer-publication, or cancellation failure leaves the previous pointer and its readable blocks intact.
3. A successful switch makes exactly one lineage active, attempts immediate deletion of previous keys, and leaves twice-interval TTLs on lineage keys for residual cleanup.
4. Failure to evict old inactive keys after a successful switch keeps the new lineage active, emits an error, and leaves TTL cleanup in place; it never rolls back to the old pointer.
5. Restart or complete Memcached loss is treated as empty ephemeral state; no file/database fallback or recovery copy exists.
6. Cache/lifecycle spans expose operation and outcome but not Memcached keys; metrics remain low cardinality and errors retain their causes.

## Validation

- Integration-test publication against Memcached, with injected failure after each write phase and concurrent readers observing the pointer.
- Unit-test deterministic blocking/reassembly, TTL calculation, pointer preservation, and eviction-key selection.
