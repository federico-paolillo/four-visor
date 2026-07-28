# US-011: Refresh with stable installation-local jitter

## Goal

Schedule browser synchronization approximately hourly using a stable, private installation-local offset.

## User Value

The Reader receives unattended refreshes without synchronized client bursts or identity/fingerprinting data leaving the browser.

## Scope

- On first activation, generate a random local seed without device/browser fingerprinting inputs, persist it in IndexedDB, and derive a stable 5–60-second jitter.
- Reuse the same offset until local reset and never transmit the seed.
- Trigger one complete synchronization at the documented approximately-hourly cadence and wait until the next cadence after any success or failure.
- Prevent overlapping browser synchronizations and continue rendering the active lineage while waiting/running.
- Document the concrete first-attempt and subsequent timer arithmetic while preserving the locked approximately-hourly stable-offset behavior.

## Out of Scope

- User-configurable preferences, server-side client state, notifications, immediate retry loops, background sync APIs not required by the SEED, or client-triggered backend acquisition.

## Dependencies

- US-010.

## Related MADRs

- None. Exact timer arithmetic is a local implementation detail; the stable, private, approximately-hourly behavior is locked.

## Traceability

- `Full Requirements / Client synchronization` (`docs/SEED.md:117-127`): approximately hourly refresh and stable installation-local jitter.
- `Operational Flows / Client synchronization` and `/ First installation jitter` (`docs/SEED.md:624-679`): next-scheduled retry, random local seed, privacy, reset lifecycle, and stable derivation.
- `Axioms` (`docs/SEED.md:40-79`): anonymous operation and only synchronization/offline local state.
- `Locked Decisions / Product` and `/ Synchronization` (`docs/SEED.md:2101-2114`, `2160-2168`): no identity/preferences and stable complete refresh.

## Acceptance Criteria

1. A fresh installation stores a randomly generated seed and deterministically derives a jitter in the inclusive 5–60-second range; reloads reuse the same value.
2. The seed uses no fingerprinting/device attributes, is absent from snapshot requests and telemetry, and is removed by US-009 reset.
3. The selected documented cadence produces approximately one attempt per hour, never overlaps attempts, and does not accumulate a new random offset each cycle.
4. Success and all failure classes wait for the next scheduled interval rather than starting an immediate automatic retry.
5. Existing local content remains readable throughout timer wait and synchronization.

## Validation

- Unit-test seed derivation range/stability/privacy, reset regeneration, cadence, no-overlap, and post-failure scheduling with a fake clock and deterministic randomness.
- Integration-test the scheduler against the US-010 synchronization boundary without making real network calls.
