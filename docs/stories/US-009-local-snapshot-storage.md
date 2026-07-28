# US-009: Start from, fail on, or reset local snapshot storage

## Goal

Make IndexedDB the mandatory and exclusive home for local snapshots, with immediate startup from the active lineage and a complete user-controlled reset.

## User Value

The Reader can open the last snapshot immediately, gets a clear explanation when required storage is unusable, and can recover from corruption by resetting all 4Visor-local data.

## Scope

- Open the application IndexedDB database at startup and load the active lineage without waiting for the backend.
- Store one active lineage plus, only during synchronization, one incoming lineage as locally keyed fixed-size serialized records; upstream `lineageId` is payload metadata and never the local record key.
- Show a clear mandatory-storage error for unavailable/corrupt IndexedDB, with no memory-only or online-only fallback.
- Show a clear empty state when no active lineage exists.
- Provide a confirmed “Reset local data” action that deletes application IndexedDB data, incoming data, jitter seed, and application shell caches, then reloads.
- Ensure reset performs no server-side call and document its local-only effect.

## Out of Scope

- Downloading/activating a replacement, periodic refresh, browser HTTP cache deletion, server cache reset, or storage migration across snapshot schema versions.

## Dependencies

- US-008.

## Related MADRs

- [MADR 0002](../madrs/0002-browser-lineage-records.md), “Store browser lineages as locally keyed fixed-size records.” Corruption detection and database naming/version mechanics remain local; version-1 migration stays excluded.

## Traceability

- `Full Requirements / Local storage` (`docs/SEED.md:128-139`): IndexedDB-only snapshots, mandatory failure, reset, and quota semantics boundary.
- `High-Level Architecture / Client architecture` (`docs/SEED.md:269-305`): one active plus temporary incoming lineage and immediate local rendering.
- `Operational Flows / Client startup` (`docs/SEED.md:599-623`): mandatory open, immediate active render, empty state, and no fallback.
- `Operational Flows / Local reset` (`docs/SEED.md:1005-1026`): confirmation, database/cache/seed removal, reload, and local-only effect.
- `Failure Semantics / Client failures` (`docs/SEED.md:1655-1665`): unavailable/corrupt IndexedDB and recovery behavior.

## Acceptance Criteria

1. Startup with a valid active lineage reads it from IndexedDB and makes it available to the UI before any backend request is required.
2. Startup with no active lineage shows the explicit empty state; unavailable or corrupt IndexedDB shows a clear blocking storage error and does not use memory/online fallback.
3. The storage model can hold no more than one active and one incoming lineage, keeps their local keys distinct even when their upstream IDs match, and never puts snapshot payloads in Cache Storage.
4. Confirmed reset removes active/incoming lineages, jitter seed, and all 4Visor application-shell caches, performs no server request, and reloads; cancellation changes nothing.
5. Reset documentation tells the Reader that cached snapshot continuity and the stable local jitter will be lost.

## Validation

- Integration-test startup with valid, empty, unavailable, and corrupt IndexedDB using a browser-storage test double, including a representative maximum-cardinality fixture large enough to span multiple records.
- Integration-test confirmed/cancelled reset across IndexedDB and Cache Storage and assert that no fetch occurs.
