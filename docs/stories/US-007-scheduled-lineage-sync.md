# US-007: Build and activate lineages on the backend schedule

## Goal

Join acquisition and publication into one instance-local, observable synchronization lifecycle that produces a new lineage from scratch on schedule.

## User Value

The Reader receives regularly refreshed snapshots even when some upstream resources fail, while the Operator can distinguish successful, degraded, and failed construction.

## Scope

- Run one synchronization at a time using `FOURVISOR_` configuration, defaulting to every four hours, after a stable instance-local startup jitter between 5 and 60 seconds.
- At construction start, create a new ULID and capture `observedAt` as UTC RFC 3339; enforce the configured lineage deadline, four hours by default.
- Acquire boards, catalogs, and threads from scratch, validate the final contract, publish atomically, and evict the prior lineage only after success.
- Activate every successfully constructed/published lineage regardless of failed-resource count, including total 4chan outage represented by `boards.state = failed`.
- Preserve the active lineage for construction, validation, cache-write, publication, and cancellation failures.
- Apply a configurable failed-resource tolerance, defaulting to 10, only to observability: above tolerance, still activate, mark the synchronization root span error, log prominently, and retain lineage ID/failed/tolerated counts as trace/log attributes.
- Skip a scheduled tick when a synchronization is already active; do not overlap, queue, or cancel the current run, and record the skip as one meaningful scheduler event.
- Emit synchronization root spans, all required child operations, minimal lineage metrics, and meaningful start/completion/activation/eviction/acquisition-summary logs.
- Document cadence, jitter persistence scope, degradation threshold, cache-loss behavior, and next-scheduled-attempt recovery.

## Out of Scope

- Manual/client-triggered synchronization, overlapping runs, background repair/replay, distributed scheduling, guaranteed completion, or rejection based solely on degradation.

## Dependencies

- US-005.

## Related MADRs

- [MADR 0005](../madrs/0005-overlapping-sync-ticks.md), “Skip overlapping backend synchronization ticks.” Jitter derivation, degradation-tolerance defaults, and scheduler implementation mechanics remain local configuration/code choices.

## Traceability

- `Full Requirements / Snapshot model`, `/ Upstream acquisition`, and `/ Failure handling` (`docs/SEED.md:98-107`, `168-190`): independent lineages, cadence, jitter, deadline, activation, and preservation rules.
- `Operational Flows / Scheduled backend synchronization` (`docs/SEED.md:680-706`): ULID, deadline, acquisition order, publication, activation, eviction, and one-run behavior.
- `Operational Flows / Degraded lineage completion` (`docs/SEED.md:868-895`): observability-only tolerance and activation despite degradation.
- `Operational Flows / Trace flow for scheduled synchronization` and `/ Telemetry export` (`docs/SEED.md:1101-1153`): trace tree, lineage attribute, lifecycle signals, and excluded high-cardinality metrics.
- `Detailed Observability / Lineages`, `/ Logging`, `/ Error handling`, and `/ Sampling` (`docs/SEED.md:1551-1615`): duration/outcome/age metrics, meaningful logs, degraded error trace, and collector sampling intent.
- `Locked Decisions / Synchronization` and `/ Failure semantics` (`docs/SEED.md:2160-2168`, `2218-2228`): stable jitter, full replacement, degraded activation, total outage, and current-lineage preservation.

## Acceptance Criteria

1. The default schedule is every four hours, its initial 5–60-second offset is stable for one backend instance, and a tick during an active run is skipped without overlap, queueing, or cancellation; the next ordinary tick remains the next opportunity.
2. A non-default valid synchronization interval changes both scheduler cadence and the Memcached lineage TTL, which remains exactly twice that interval; invalid interval/tolerance settings fail startup clearly.
3. Each run uses a new valid ULID and one start-time UTC `observedAt`, has a configurable hard deadline that defaults to four hours, and cannot consume prior-lineage resources.
4. Successful construction/publication activates the new lineage and evicts the old; all listed non-resource failures preserve the old active pointer.
5. Upstream resource failures, including an unfinished board-list request and every known unfinished catalog/thread at the lineage deadline, produce exact failed wrappers and can activate as a contract-valid degraded lineage; external/shutdown cancellation instead preserves the active lineage.
6. More than 10 failed resources by default changes telemetry only: activation still occurs, the root span becomes error, a prominent structured log is emitted, and the full trace is eligible for failed-trace retention.
7. Lineage metrics are limited to duration, successful/degraded outcome, failed count, and active age; identifiers do not become metric labels.

## Validation

- Unit-test stable jitter range/reuse, overlap-tick skipping and its event, ULID/time assignment, threshold classification, complete/degraded/outage outcomes, deadlines, cancellation, publication failure, cache loss, and next-interval behavior with fakes.
