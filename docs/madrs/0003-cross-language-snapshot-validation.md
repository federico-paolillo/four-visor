# Govern cross-language snapshot validation with independent validators and shared fixtures

## Context

The Go producer and TypeScript consumer must agree on a strict contract. Wrapper
objects reject unknown fields and invalid state/payload combinations, while
upstream board, summary, metadata, and post objects remain opaque. The seed
requires backend validation before publication and client validation before
activation, but does not choose a cross-language contract mechanism.

## Decision

Implement explicit version-1 validators in Go at lineage publication and in
TypeScript at the network/storage activation boundary. Keep the validators
independent and idiomatic to each language. Maintain one shared, language-neutral
fixture corpus containing representative valid documents and focused invalid
documents for every strict wrapper rule; both validation suites consume the
same corpus.

Do not introduce JSON-Schema runtime engines, schema code generation, or a
compatibility/migration layer for version 1. Opaque upstream objects receive
only the object-type check required by the seed, while contract-owned wrappers
receive exact validation.

## Decision Drivers

- Enforce the exact rejection behavior in both runtimes.
- Prevent drift without adding generators and runtime schema dependencies.
- Preserve unrestricted upstream fields and values.
- Keep validation failures classified at the correct trust boundaries.
- Honor the explicit no-migration/no-compatibility-window decision.

## Considered Options

1. **Independent validators plus shared fixtures — chosen.** Small tooling
   footprint with executable cross-language agreement.
2. **Canonical JSON Schema with runtime validators.** Centralizes declaration
   but adds runtime libraries in both tiers and still requires careful opaque
   object handling.
3. **Generate Go and TypeScript models/validators from a schema.** Reduces some
   duplication but introduces a build-time toolchain and generated-code
   lifecycle disproportionate to one frozen version.
4. **Trust backend output or validate only `schemaVersion`.** Too weak for the
   seed's explicit wrapper, cardinality, timestamp, ULID, and state rules.

## Consequences

### Positive

- Each boundary fails invalid data before it becomes active.
- Shared negative fixtures expose producer/consumer interpretation drift.
- No new runtime validator or code-generation stack is required.
- Upstream payload evolution remains accepted inside opaque objects.

### Negative

- Validation logic is intentionally duplicated across Go and TypeScript.
- Every contract change would require two validator edits and fixture updates.
- Fixture coverage must be maintained carefully because it is the shared
  executable specification.

## Related User Stories

- [US-002](../stories/US-002-snapshot-v1-contract.md)
- [US-005](../stories/US-005-memcached-lineage-publication.md)
- [US-010](../stories/US-010-client-lineage-sync.md)

## Traceability

- `Full Requirements / Snapshot model`, `Client synchronization`, and `Failure
  handling`.
- `High-Level Architecture / Snapshot schema version 1` in full.
- `Operational Flows / Client synchronization` and `Lineage construction and
  activation`.
- `Detailed Observability / Error handling`.
- `Failure Semantics / Client failures`, especially schema mismatch.
- `Locked Decisions / Snapshot model` and `Failure semantics`.
