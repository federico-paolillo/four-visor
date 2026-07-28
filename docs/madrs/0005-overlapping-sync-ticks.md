# Skip overlapping backend synchronization ticks

## Context

The scheduler follows a configured fixed interval after a stable startup jitter,
and a lineage build can run for up to thirty minutes. The backend owns only one
lineage under construction. A short configured interval or delayed build can
therefore cause a new tick while work is active. The seed excludes distributed
coordination and repair queues but does not say whether the local tick queues,
cancels, or is discarded.

## Decision

Run lineage construction as a single-flight operation. Keep the fixed schedule;
when a tick occurs during an active build, skip that tick. Do not queue a later
run, overlap builds, or cancel the current build. The next ordinary scheduled
tick is the next opportunity. Record the skip as a meaningful scheduler state
event using the existing observability path, without adding a queue or retry
subsystem.

## Decision Drivers

- Preserve exactly one lineage under construction.
- Avoid competing upstream load and cache publication races.
- Keep cancellation semantics tied to shutdown/deadline, not freshness guesses.
- Avoid queues and accumulated catch-up work.
- Keep scheduling deterministic and understandable for one instance.

## Considered Options

1. **Skip overlapping ticks — chosen.** Maintains fixed cadence and bounded work
   with the least state.
2. **Queue one pending synchronization.** Reduces missed opportunities but can
   cause immediate catch-up load and introduces queue state.
3. **Cancel the current build and start the newer tick.** Wastes acquisition
   work and can starve activation when builds repeatedly run long.
4. **Allow concurrent builds.** Contradicts the one-building-lineage model and
   introduces activation races and greater upstream load.
5. **Schedule the next interval only after completion.** Avoids overlap but
   changes fixed cadence and makes refresh frequency depend on build duration.

## Consequences

### Positive

- At most one acquisition tree consumes resources or publishes cache data.
- No queue, coordinator, generation race, or cancellation handoff is needed.
- The current active lineage remains served throughout a long build.
- Default settings normally avoid the edge case because the one-hour interval
  exceeds the thirty-minute deadline.

### Negative

- Misconfigured short intervals can result in skipped refresh opportunities.
- A failed or slow build is not followed by an immediate catch-up attempt.
- Operators must use trace/log evidence to distinguish an overlap skip from a
  started synchronization.

## Related User Stories

- [US-007](../stories/US-007-scheduled-lineage-sync.md)

## Traceability

- `Axioms`: each synchronization is independent; one backend is authoritative;
  simplicity and determinism take precedence.
- `Full Requirements / Upstream acquisition`.
- `High-Level Architecture / Backend` and `Upstream acquisition`.
- `Operational Flows / Scheduled backend synchronization` and `Retry behavior`.
- `Deployment View / Scheduling`.
- `Design Notes / Single backend` and `Simplicity over flexibility`.
- `Locked Decisions / Synchronization`, `Backend`, and `Out of scope`.
