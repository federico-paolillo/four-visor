# US-002: Enforce the exact snapshot version 1 contract at both boundaries

## Goal

Give backend publication and browser synchronization one exact, fixture-backed interpretation of snapshot schema version 1.

## User Value

The Reader never activates structurally invalid or incompatible content, while valid upstream fields and ordering survive unchanged across the backend/browser boundary.

## Scope

- Define backend JSON models/validation and frontend TypeScript parsing/validation for the exact nested schema version 1 contract.
- Validate ULID lineage identifiers and UTC RFC 3339 `observedAt` values.
- Enforce all wrapper fields, state/payload combinations, cardinality limits, and unknown-wrapper-field rejection.
- Preserve unrestricted board, page metadata, thread-summary, and post fields and values as opaque upstream objects.
- Maintain shared valid and invalid contract fixtures usable by Go and Vitest integration tests.
- Document that version 1 has no migration, adapter, compatibility window, route version, or partial acceptance.
- Before frontend work begins, align the repository validation task with the workflow-mandated `mise run fe:check` by replacing the current `fe:typecheck` task name.

## Out of Scope

- Upstream fetching, Memcached layout, HTTP serving, IndexedDB persistence, HTML sanitization, or a future schema version.

## Dependencies

- None.

## Related MADRs

- [MADR 0003](../madrs/0003-cross-language-snapshot-validation.md), “Govern cross-language snapshot validation with independent validators and shared fixtures.” The exact schema itself is locked; the decision governs cross-language enforcement without restating the contract.

## Traceability

- `High-Level Architecture / Snapshot contents` (`docs/SEED.md:348-374`): lineage contents, failed/oversize representations, unchanged HTML/media references, and binary/user-state exclusions.
- `High-Level Architecture / Snapshot schema version 1` (`docs/SEED.md:429-495`): exact root, wrappers, opaque objects, ordering, cardinality, ULID/time validation, and version rejection semantics.
- `Locked Decisions / Snapshot model` and `/ Backend cache` (`docs/SEED.md:2115-2134`): exact version 1, no compatibility window, 250 limits, unchanged HTML, and binary exclusion.
- `Technology Stack / Data formats` (`docs/SEED.md:1792-1798`): JSON, unchanged HTML, and ULID identifiers.

## Acceptance Criteria

1. Both implementations accept the same canonical valid fixtures for present, failed, absent, and oversize resource combinations without changing opaque upstream fields or array order.
2. Both reject missing/extra wrapper fields, wrong types, invalid states, invalid state/payload combinations, invalid ULIDs, non-UTC timestamps, and catalog/thread cardinalities above 250.
3. `boards`, catalog, and thread failure wrappers contain no payload or failure-detail string; unexplained absence is represented only by the permitted missing optional field.
4. Schema versions other than integer `1`, including a missing version, are rejected explicitly.
5. Contract documentation matches the fixtures and states that no migration or fallback parsing exists.
6. `mise run fe:build`, `mise run fe:test`, `mise run fe:lint`, and `mise run fe:check` are defined and pass; later frontend stories reuse those exact workflow tasks.

## Validation

- Run table-driven Go unit tests and Vitest unit tests over the same valid/invalid fixture corpus.
- Add a cross-boundary integration fixture proving backend serialization is accepted by the frontend parser without losing opaque fields or ordering.
