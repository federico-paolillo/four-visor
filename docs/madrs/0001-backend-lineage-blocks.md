# Store backend lineages as ordered fixed-size serialized blocks

## Context

The public contract is one nested JSON response, while Memcached values have a
per-item ceiling and a full lineage can be much larger than one item. The seed
requires all blocks and completion metadata to exist before activation, and a
missing required block must produce `410 Gone`. It deliberately leaves the
internal block layout open.

## Decision

Serialize the exact schema-version-1 logical JSON document to UTF-8 and divide
that byte stream into ordered fixed-size blocks, each safely below the deployed
Memcached item ceiling. Store immutable blocks under the lineage namespace.
Store completion metadata containing at least the ordered block count and total
byte length. The active pointer continues to contain only the lineage identity.

Before returning `200`, the snapshot handler resolves completion metadata and
confirms every block is present. It then concatenates/streams blocks in order as
one JSON entity. The precise safe block size and key spelling are implementation
details.

## Decision Drivers

- Remain below Memcached per-item limits.
- Preserve the exact one-response public contract.
- Make atomic publication a single pointer change.
- Detect incomplete/evicted active lineages deterministically.
- Keep the cache representation independent of board/thread structure.
- Avoid a database, public block protocol, or custom binary format.

## Considered Options

1. **Ordered fixed-size serialized blocks — chosen.** Minimal reconstruction
   logic and no oversized resource special case.
2. **One Memcached value per lineage.** Simplest, but cannot safely satisfy
   ordinary item ceilings for the required data volume.
3. **Resource-aligned board/catalog/thread blocks.** Natural domain keys, but
   creates many cache operations, still needs subdivision for a large resource,
   and couples serving to schema assembly.
4. **Persistent file/database backing.** Avoids item limits but contradicts the
   locked ephemeral-cache and no-durable-backend-state architecture.

## Consequences

### Positive

- One generic chunking rule handles every lineage size up to overall capacity.
- Public clients remain unaware of cache partitioning.
- Completion is easy to determine from immutable metadata and block presence.
- Schema evolution, if ever authorized, does not require a new cache topology.

### Negative

- Individual resources are not independently readable from cache.
- The backend must finish serialization before publication.
- Serving must resolve/check all blocks before committing the HTTP success
  status, increasing per-request memory or cache-operation pressure.
- Memcached still provides no durability; eviction correctly produces `410`.

## Related User Stories

- [US-005](../stories/US-005-memcached-lineage-publication.md)
- [US-006](../stories/US-006-snapshot-serving.md)
- [US-007](../stories/US-007-scheduled-lineage-sync.md)

## Traceability

- `Axioms`: immutable lineages; Memcached is an ephemeral serving cache.
- `Full Requirements / Snapshot model`: atomic activation and no partial
  visibility.
- `Full Requirements / Backend cache`: complete textual resource scope.
- `High-Level Architecture / Backend`: lineage blocks, active pointer, TTL,
  missing block behavior.
- `High-Level Architecture / Snapshot transfer`: one logical JSON response;
  internal multi-block storage permitted.
- `Operational Flows / Lineage construction and activation`: write every block,
  completion metadata, pointer switch, eviction.
- `Operational Flows / Serving a snapshot`: all blocks required or `410 Gone`.
- `Design Notes / Memcached as a serving cache`.
