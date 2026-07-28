# US-010: Replace the local lineage only after complete synchronization

## Goal

Download, validate, stage, and atomically activate one backend-authoritative snapshot while preserving the Reader's current lineage on every failure.

## User Value

The Reader can continue reading a complete snapshot during refresh and after network, backend, schema, or storage failures.

## Scope

- Fetch one logical snapshot from browser route `GET /api/snapshot` only when a synchronization is due.
- Parse and validate the entire payload against exact schema version 1, stage it in temporary IndexedDB storage, then atomically switch the active pointer.
- Keep the old active lineage visible until commit succeeds; after success delete the previous lineage and render the replacement.
- Accept the backend's lineage without timestamp/ULID comparison, merge, reconciliation, or partial activation.
- On network/HTTP/`410`, parse/schema, quota, IndexedDB, or activation failure, keep the current lineage, leave incoming data inactive/cleanable, show a clear classified error, and wait for the next scheduled attempt.
- Propagate abort/cancellation and avoid requests for individual missing resources.
- Document incompatible-deployment, quota, server-unavailable, and first-sync behavior.

## Out of Scope

- Periodic timer/jitter policy, resumable downloads, multiple public blocks, differential sync, migration/adapters, background retry, or resource-specific backend calls.

## Dependencies

- US-006.
- US-009.

## Related MADRs

- [MADR 0002](../madrs/0002-browser-lineage-records.md), “Store browser lineages as locally keyed fixed-size records,” for staging, validation, promotion, and removal without same-ID aliasing.
- [MADR 0003](../madrs/0003-cross-language-snapshot-validation.md), “Govern cross-language snapshot validation with independent validators and shared fixtures,” for activation validation. One-response transfer is already the locked initial interpretation, not another MADR.

## Traceability

- `Full Requirements / Client synchronization` and `/ Local storage` (`docs/SEED.md:117-139`): complete-before-activation, prior preservation, one active lineage, IndexedDB, and quota handling.
- `High-Level Architecture / Client architecture` and `/ Snapshot transfer` (`docs/SEED.md:269-305`, `395-428`): stage, validate, atomic swap, one logical response, and browser decompression.
- `Operational Flows / Client synchronization` (`docs/SEED.md:624-658`): exact success/failure path and backend authority.
- `Failure Semantics / Client failures` and `/ Synchronization failures` (`docs/SEED.md:1655-1681`): network/backend/schema/storage outcomes and no partial activation.
- `Locked Decisions / Snapshot model` and `/ Synchronization` (`docs/SEED.md:2115-2125`, `2160-2168`): no reconciliation/compatibility, complete replacement, and prior retention.

## Acceptance Criteria

1. During download, validation, and staging, reads/rendering continue to resolve the old active lineage.
2. A valid complete response is staged, committed with one atomic active-pointer change, followed by previous-lineage deletion; only one active lineage remains.
3. Network failure, non-success HTTP including `410`, invalid JSON/schema, incompatible version, quota/storage failure, and cancellation leave the old active lineage unchanged and produce distinct clear user-facing errors.
4. A valid backend lineage is activated even if its identifier/time is older than the local one; no merge, comparison, or partial progressive rendering occurs.
5. A valid response with the same upstream lineage ID as the active document stages under a distinct local key and activates safely; validation or transaction failure cannot overwrite or delete the old active records.
6. The client makes no textual-content request other than the complete snapshot request and schedules no immediate automatic retry.

## Validation

- Integration-test every stage with injected failure and concurrent active reads, including successful and failed same-ID refreshes, asserting pointer/data invariants and user error classification.
- Unit-test response classification, backend-authority behavior, abort propagation, and old/incoming cleanup decisions.
