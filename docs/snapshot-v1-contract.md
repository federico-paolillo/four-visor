# Snapshot version 1 implementation contract

The backend and browser independently enforce the snapshot contract defined in
[`SEED.md`](SEED.md). Their shared executable examples live in
[`../testdata/snapshot-v1`](../testdata/snapshot-v1): both test suites accept
every `valid` fixture and reject the categorized negative fixtures.

Contract-owned objects are exact. The root, board-list wrapper, board item,
catalog wrapper, page, thread entry and thread wrapper reject missing, unknown
or wrongly typed fields. Failed wrappers contain only `state`; present wrappers
contain their required payload; an oversize thread contains exactly 250 posts.
Catalog pages contain at most 250 thread entries in total, and present threads
contain at most 250 posts.

An omitted `catalog` or `thread` means unexplained absence. These optional
resources may be omitted but may not be `null`. Failed resources contain no
payload or failure-detail field.

The upstream `board`, page `metadata`, thread `summary` and post values are
opaque objects. Validation checks only that each is a non-null, non-array JSON
object. It does not restrict, rename, reorder or discard their fields or nested
values. All wrapper and payload arrays preserve source order and page
boundaries.

`schemaVersion` is the integer `1`. Lineage identifiers are valid 26-character
ULIDs, parsed case-insensitively and preserved as supplied. `observedAt` is a
valid RFC 3339 timestamp in UTC, written with `Z` or `+00:00`; nonzero offsets
and the unknown-offset spelling `-00:00` are rejected.

Version 1 has no migration, adapter, compatibility window, versioned route,
fallback parser or partial acceptance. Any invalid or unsupported incoming
snapshot is rejected as a whole.
