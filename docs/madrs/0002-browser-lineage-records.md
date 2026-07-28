# Store browser lineages as locally keyed fixed-size records

## Context

The browser downloads and validates one complete nested document and renders
from one active local lineage. IndexedDB must hold both the current active
lineage and, transiently, one inactive incoming candidate. The seed fixes the
activation semantics but not whether the client normalizes resources, stores
transport chunks, or stores the logical document intact.

## Decision

Store each lineage as ordered fixed-size serialized records under a locally
generated storage key, plus completion metadata and one active-storage-key
pointer. The local key is independent of the upstream `lineageId`, which remains
validated payload metadata. This keeps the active and incoming records distinct
even when the backend serves the same lineage identifier again.

Write every incoming record and its completion metadata without changing the
active pointer, then reassemble, parse, and validate the staged document.
Promote it in one IndexedDB read-write transaction that changes the active
pointer and deletes the previous lineage records. Transaction failure leaves
the previous pointer and records intact. Rendering reads only the generation
named by the committed active pointer.

The exact safe record size, object-store names, and local-key format remain
implementation details. Do not normalize boards, catalogs, threads, or posts;
the browser still stores and activates one opaque logical document.

## Decision Drivers

- Match the one-document transfer and immutable-lineage mental model.
- Avoid one large structured-clone record for the seed's potentially large
  complete snapshot.
- Make activation and old-lineage removal transactionally simple.
- Ensure inactive or invalid data is never renderable.
- Accept same-ID refreshes without local storage-key aliasing.
- Minimize schema-specific IndexedDB code and migration surface.
- Keep the personal-project implementation small.

## Considered Options

1. **Locally keyed fixed-size serialized records — chosen.** Avoids a single
   large structured clone while retaining generic reassembly and one pointer
   transaction.
2. **One structured-cloned record per lineage.** Has the smallest record model,
   but the seed provides no defensible upper bound for the complete document.
3. **Normalize boards, catalogs, threads, and posts into stores.** Enables
   selective reads but creates indexes, joins, bulk cleanup, and schema-coupled
   migration logic that the initial client does not require.
4. **Cache Storage or in-memory storage.** Directly violates mandatory storage
   boundaries and offline continuity.

## Consequences

### Positive

- Fixed-size records avoid depending on a browser-specific maximum record size.
- Local generation keys make same-ID refreshes safe.
- Atomic promotion has one clear transaction boundary.
- Reset and failed-candidate cleanup are straightforward.
- No client-side entity reconstruction or lineage reconciliation exists.

### Negative

- Startup and activation must reassemble the complete serialized document.
- The active document may still occupy substantial browser memory while
  parsing and rendering.
- IndexedDB operations touch more records than a monolithic-value design.
- Validation code must avoid accidentally rendering the inactive record.

## Related User Stories

- [US-009](../stories/US-009-local-snapshot-storage.md)
- [US-010](../stories/US-010-client-lineage-sync.md)

## Traceability

- `Axioms`: clients render one complete local lineage; browser is primary serving
  layer; operational local state only.
- `Full Requirements / Client synchronization` and `Local storage`.
- `High-Level Architecture / Client architecture`.
- `High-Level Architecture / Snapshot transfer` and `Snapshot schema version 1`.
- `Operational Flows / Client startup`, `Client synchronization`, `First
  installation jitter`, and `Local reset`.
- `Design Notes / Client-first design` and `No incremental synchronization`.
- `Failure Semantics / Client failures` and `Synchronization failures`.
