# US-006: Serve the active lineage as one logical snapshot

## Goal

Expose the active Memcached lineage through the backend's only snapshot route with unambiguous cache-loss semantics.

## User Value

The Reader can download one complete JSON snapshot, while an unavailable or expired server snapshot is reported clearly enough to preserve the browser's local copy.

## Scope

- Serve internal `GET /snapshot` using the active pointer, completion metadata, and every required lineage block.
- Verify all required blocks before committing success, then stream/reassemble exactly one logical schema-version-1 JSON response without adding a public manifest, block, range, or resource endpoint.
- Return `410 Gone` if the pointer, completion metadata, or any required block is absent/incomplete; propagate other dependency/request failures meaningfully.
- Mark snapshot responses `Cache-Control: no-store` so neither intermediaries nor the Service Worker become an alternate textual serving cache.
- Do not apply Brotli compression in Go; leave normal HTTP content encoding to the VPS ingress.
- Reuse the US-001 HTTP server root span and add snapshot-specific cache-read and serialization children, status, request metrics, and error-only logs; propagate request cancellation without creating a nested server root.
- Document `200`/`410` semantics and the internal route contract.

## Out of Scope

- `/api` prefix handling, Brotli ownership, client synchronization, resumable/fixed-block public transfer, individual board/thread endpoints, or upstream acquisition triggered by a request.

## Dependencies

- US-005.

## Related MADRs

- [MADR 0001](../madrs/0001-backend-lineage-blocks.md), “Store backend lineages as ordered fixed-size serialized blocks,” which determines ordered reassembly and complete-block verification before a successful response. Buffer/stream mechanics remain local.

## Traceability

- `High-Level Architecture / HTTP routing` and `/ Snapshot transfer` (`docs/SEED.md:375-428`): internal route, logical response, ingress-owned Brotli, and excluded transfer mechanisms.
- `Operational Flows / Serving a snapshot` (`docs/SEED.md:840-867`): lookup sequence and `410 Gone` for any missing active component.
- `Failure Semantics / Cache failures` (`docs/SEED.md:1702-1713`): expired/unavailable snapshot meaning rather than resource `404`.
- `Operational Flows / Trace flow for inbound requests` (`docs/SEED.md:1081-1100`): root, pointer/metadata/block/serialization child spans, and error propagation.
- `Locked Decisions / Backend` (`docs/SEED.md:2169-2191`): browser/backend route mapping and ingress-only Brotli.

## Acceptance Criteria

1. After verifying every referenced block and before committing response headers, a complete active lineage returns `200` and one JSON document that passes both US-002 validators and preserves ordering/opaque values.
2. Missing pointer, metadata, or any referenced block returns `410` and never emits a partial successful document.
3. The backend exposes no manifest, block, range, per-board, per-thread, or acquisition-triggering route.
4. The response is not Brotli-compressed by the backend; request cancellation stops cache reads/serialization promptly.
5. Traces contain the required cache and serialization children; a failed request marks the relevant/root spans and logs the error without logging raw payloads or keys.
6. The snapshot response is explicitly non-cacheable by HTTP intermediaries/Service Worker and remains absent from Cache Storage.

## Validation

- Integration-test exact `200` reconstruction against a fresh Memcached instance.
- Unit-test missing and corrupt components, method/route behavior, cancellation, serialization failure, telemetry attributes, and absence of application-level Brotli.
