# Independent Planning Review — Round 1

## Result

CHANGES REQUESTED. The planning set has 9 findings: 1 high, 3 medium, and 5 low severity.

The review independently read `AGENTS.md`, `DECOMPOSE.md`, all of `docs/SEED.md`, every final MADR and User Story, `docs/TODO.md`, `.planning/TRACEABILITY.md`, `.planning/OPEN_QUESTIONS.md`, and `.planning/AGENT_REPORT.md`. Specialist drafts were not used as proof.

## Findings

### R1-01 — High — The first frontend story cannot satisfy the mandated validation workflow

**Evidence**

- `AGENTS.md:115-120` requires every frontend change to pass `mise run fe:build`, `mise run fe:test`, `mise run fe:lint`, and `mise run fe:check` before handoff.
- `docs/stories/US-002-snapshot-v1-contract.md:13-17` and `:47-50` require TypeScript/Vitest implementation and validation, so US-002 is a frontend story.
- `docs/TODO.md:19` schedules US-002 before US-008 at `docs/TODO.md:25`.
- `docs/TODO.md:12` and `docs/stories/US-008-offline-pwa-shell.md:19` defer creation/renaming of `fe:check` to US-008.
- The current task is still named `fe:typecheck` at `mise.toml:26-28`; no `fe:check` task exists.

**Impact**

US-002 cannot reach a valid Worker handoff: it must run a workflow command that the plan creates six stories later. This violates independent completability and makes the TODO order unusable under `AGENTS.md`.

**Minimal resolution**

Move ownership of the `fe:typecheck` to `fe:check` task alignment from US-008 to US-002, the first frontend-changing story. Update `docs/TODO.md:12`, US-002 scope/acceptance, and US-008 scope so US-008 verifies but does not first create the mandated task.

### R1-02 — Medium — The image-packaging story hides the completed application as an undeclared prerequisite

**Evidence**

- `docs/stories/US-016-first-party-images.md:5`, `:13-15`, and `:41-48` package the production Go backend and production PWA.
- Its dependency list at `docs/stories/US-016-first-party-images.md:24-27` names only US-006 and US-008.
- Backend scheduling is completed separately by US-007 (`docs/stories/US-007-scheduled-lineage-sync.md:11-21`), client refresh by US-011 (`docs/stories/US-011-client-refresh-jitter.md:11-17`), and the final reader/media path by US-015 (`docs/stories/US-015-direct-media-loading.md:11-18`). None is reachable from US-016's declared dependencies.
- `docs/TODO.md:24-33` happens to place those capabilities before US-016, so the checklist order is carrying hidden prerequisites that the story graph omits.

**Impact**

US-016 is not independently completable as the final production-image slice from its declared prerequisites. A coordinator using story dependencies rather than the current list order could package incomplete backend and frontend products.

**Minimal resolution**

Declare the direct branch leaves needed by the production artifacts: US-006 and US-007 for the backend, plus US-011 and US-015 for the frontend. Remove US-008 because it is then transitive.

### R1-03 — Medium — Four dependency lists contain transitive rather than direct prerequisites

**Evidence**

- US-005 lists US-002 and US-004 at `docs/stories/US-005-memcached-lineage-publication.md:26-30`, although US-004 depends on US-003 (`docs/stories/US-004-thread-acquisition.md:24-26`) and US-003 already depends on US-002 (`docs/stories/US-003-board-catalog-acquisition.md:26-29`).
- US-006 lists US-001 and US-005 at `docs/stories/US-006-snapshot-serving.md:25-28`, although US-005 reaches US-001 through US-004 and US-003.
- US-011 lists US-009 and US-010 at `docs/stories/US-011-client-refresh-jitter.md:23-26`, although US-010 already depends on US-009 at `docs/stories/US-010-client-lineage-sync.md:25-28`.
- US-017 lists US-001 and US-016 at `docs/stories/US-017-compose-deployment.md:25-28`, although US-016 reaches US-001 through US-006, US-005, US-004, and US-003.
- `.planning/TRACEABILITY.md:236` nevertheless states that only direct prerequisites are listed.

**Impact**

The dependency graph contradicts its traceability conclusion and obscures the actual transitive reduction requested by the planning contract. The TODO order remains topologically valid, but the dependency metadata is not exact.

**Minimal resolution**

Remove US-002 from US-005, US-001 from US-006, US-009 from US-011, and US-001 from US-017; then regenerate the dependency conclusion in traceability after applying R1-02.

### R1-04 — Medium — Deadline classification is explicit for catalogs and threads but not for the board-list resource

**Evidence**

- `docs/SEED.md:189`, `:500-510`, and `:1731-1733` require every unfinished resource at the lineage deadline to be marked failed so the degraded lineage can complete.
- `docs/stories/US-003-board-catalog-acquisition.md:16` distinguishes board-list technical failure but says only known unfinished catalogs become failed at the lineage deadline; acceptance criterion 5 at `:49` again covers catalogs only.
- US-004 correctly covers unfinished threads at `docs/stories/US-004-thread-acquisition.md:16-17` and `:45`.
- US-007 acceptance criterion 5 at `docs/stories/US-007-scheduled-lineage-sync.md:50` again names unfinished catalogs/threads but not an unfinished board-list request.

**Impact**

An implementation can reasonably classify a lineage-deadline cancellation during board acquisition as a construction cancellation that preserves the old lineage, rather than as `boards.state = failed` followed by degraded activation. That differs from the seed's deadline flow.

**Minimal resolution**

Add the board-list resource explicitly to US-003 and US-007 deadline scope, acceptance criteria, and deadline integration cases: if unfinished at the lineage deadline, it becomes the exact `boards.state = failed` wrapper; external/shutdown cancellation still aborts activation and preserves the current lineage.

### R1-05 — Low — Telemetry ownership language remains ambiguous despite the Agent Report's claimed split

**Evidence**

- `.planning/AGENT_REPORT.md:34` says US-001 owns SDK/health instrumentation and US-018 owns Collector sampling and cross-capability verification.
- US-001 establishes inbound HTTP root spans at `docs/stories/US-001-backend-health-boundary.md:16`, and acceptance criterion 5 at `:46` requires every inbound request, including rejected methods/routes, to use that boundary.
- US-006 says it will create an HTTP root span at `docs/stories/US-006-snapshot-serving.md:18` without saying that it reuses the US-001 boundary.
- US-018 correctly says not to reimplement accepted instrumentation at `docs/stories/US-018-telemetry-collector.md:15`, but US-017 validation still broadly owns “Collector configuration” at `docs/stories/US-017-compose-deployment.md:54` while US-018 owns configuring the Collector pipeline at `docs/stories/US-018-telemetry-collector.md:13-18`.

**Impact**

Workers could read the stories as permission to create nested/duplicate server roots or divide Collector pipeline changes differently from the stated ownership, causing duplicate work and inconsistent trace structure.

**Minimal resolution**

Change US-006 to reuse the US-001 HTTP root and add only snapshot-specific child spans/status. Limit US-017 to Collector service wiring, config mounting, and compliant ports; state that receiver/pipeline/filtering/sampling/export semantics and their validation belong only to US-018.

### R1-06 — Low — The global port assumption also reads as applying to external targets

**Evidence**

- `docs/TODO.md:11` says every service, listener, **target**, container, and published port uses 65100–65199 without limiting the statement to project-controlled services.
- The seed requires external HTTPS/upstream communication at `docs/SEED.md:1171`, `:1808-1816`; those remote ports are not controlled by this project.
- US-017 uses the intended narrower scope—edge, frontend, backend, Memcached, and Collector ports—at `docs/stories/US-017-compose-deployment.md:13-18` and `:45`.

**Impact**

Literal application of the TODO assumption conflicts with external 4chan, ingress, and telemetry-destination ports and can misdirect implementation or review.

**Minimal resolution**

Qualify `docs/TODO.md:11` as applying to project-controlled local/Compose service listeners, proxy targets, container ports, health-check ports, and published ports. Keep remote upstream, ingress, and operator exporter destinations outside that range rule.

### R1-07 — Low — Final traceability names temporary specialist drafts as mapped authority

**Evidence**

- `.planning/TRACEABILITY.md:5-7` says the final matrix maps `.planning/ARCHITECTURE.md` and `.planning/STORIES.md` alongside authoritative/final inputs.
- `DECOMPOSE.md:67-75` defines specialist files under `.planning/` as temporary findings and reserves final deliverables to the Coordinator.

**Impact**

The final proof can be read as depending on stale synthesis drafts rather than only the seed, final MADRs, final stories, explicit assumptions, and TODO. This weakens exact traceability even though the matrix rows themselves reference final artifacts.

**Minimal resolution**

Remove the two specialist drafts from `.planning/TRACEABILITY.md:5-7` and state that the matrix maps `docs/SEED.md` to final MADRs, final stories, `docs/TODO.md` assumptions, and final resolved interpretations.

### R1-08 — Low — The required Agent Report is still incomplete

**Evidence**

- `DECOMPOSE.md:266-273` requires `.planning/AGENT_REPORT.md` to record review findings and resolutions.
- `.planning/AGENT_REPORT.md:10`, `:39-41`, and `:43-45` still show the Review Specialist and review findings as pending and contain no round-1 resolutions.

**Impact**

One required planning deliverable is not final or internally current after this review handoff.

**Minimal resolution**

After resolving or explicitly deferring this report, update the Review Specialist artifact, findings, resolutions, and remaining open questions in `.planning/AGENT_REPORT.md`; do not leave “Pending independent review.”

### R1-09 — Low — US-015 leaves its MADR relationship tentative while declaring no open questions

**Evidence**

- `docs/stories/US-015-direct-media-loading.md:28-30` says “None likely” under Related MADRs.
- `.planning/OPEN_QUESTIONS.md:3` says no open questions remain.

**Impact**

The final story retains synthesis uncertainty about whether another architectural decision is required, contradicting the declared finality of the planning set.

**Minimal resolution**

Replace “None likely” with a definitive “None” and retain the existing explanation that the behavior is locked and native elements cover local implementation.

## Checks With No Finding

- All five MADRs document cohesive implementation choices left open by the seed; none merely restates an axiom or locked direction, duplicates another MADR, or contains a lifecycle/status field.
- No complete User Story duplicates another, and story completion state appears only as unchecked boxes in `docs/TODO.md`.
- Locally generated browser storage keys, fixed-size records, same-ID staging, same-ID activation, and failure preservation are consistent across MADR 0002, US-009, US-010, TODO assumptions, traceability, and the Agent Report.
- The one-request-per-second assumption matches the currently cited official 4chan API rule. Retry count/backoff and degradation tolerance are explicit defaults, remain bounded/configurable at the documented boundary, and do not change degradation activation semantics.
- Frontend validation command names are consistent everywhere except for the ordering blocker in R1-01.
- Listed TODO dependencies never point forward; the remaining dependency defects are the hidden and transitive edges in R1-02/R1-03.
- Port-range, loopback-only exposure, no-firewall-change, first-party hardening, third-party exemption, and unit/integration-only test constraints are otherwise represented consistently.
- Locked product, platform, media, storage, distributed-system, routing, test, and observability exclusions remain excluded.
- All required planning paths exist, all TODO boxes are unchecked, and no application source change is part of the planning output. The Agent Report content issue is limited to R1-08.
