# Independent Planning Review — Round 2

## Result

CHANGES REQUESTED. Round 2 found 6 issues: 1 high, 3 medium, and 2 low severity.

## Round-1 Resolution Verification

| Round-1 finding | Result | Evidence |
| --- | --- | --- |
| R1-01 — `fe:check` ownership/order | Partial; R2-01 and R2-02 | US-002 now owns the rename at `docs/stories/US-002-snapshot-v1-contract.md:19,47`, but the rename was also implemented during this planning-only task and US-008 does not declare its new dependency on US-002. |
| R1-02 — US-016 hidden branch prerequisites | Partial; R2-03 | US-016 now lists US-006, US-007, US-011, and US-015 at `docs/stories/US-016-first-party-images.md:24-29`, but US-006 is transitive through US-011/US-015. |
| R1-03 — transitive dependency edges | Partial; R2-02 and R2-03 | The four reported edges were removed, but the R1-01/R1-02 fixes introduced or exposed new graph defects. |
| R1-04 — board-list deadline classification | Resolved | `docs/stories/US-003-board-catalog-acquisition.md:16,48,55` and `docs/stories/US-007-scheduled-lineage-sync.md:50,57` distinguish lineage-deadline degradation from external/shutdown cancellation and validate both. |
| R1-05 — HTTP-root/Collector ownership | Resolved | US-006 explicitly reuses the root at `docs/stories/US-006-snapshot-serving.md:18`; US-017 owns wiring only at `docs/stories/US-017-compose-deployment.md:19,25,55`; US-018 owns pipeline semantics at `docs/stories/US-018-telemetry-collector.md:13-16`. |
| R1-06 — local port scope | Partial; R2-05 | `docs/TODO.md:11` is correctly scoped, but `.planning/TRACEABILITY.md:229` and the Agent Report summary were not kept exact. |
| R1-07 — final-only traceability | Partial; R2-04 | The scope statement is corrected at `.planning/TRACEABILITY.md:5-8`, but two matrix mappings still refer to the temporary architecture draft implicitly. |
| R1-08 — Agent Report currency | Partial; R2-06 | Round-1 findings/resolutions are recorded, but an earlier row contradicts the final `fe:check` owner and the direct-dependency claim is currently false. |
| R1-09 — definitive Related MADRs wording | Resolved | `docs/stories/US-015-direct-media-loading.md:28-30` now states definitive “None.” |

## Findings

### R2-01 — High — The planning-only remediation implemented the future US-002 tooling change

**Evidence**

- `DECOMPOSE.md:10` defines this as planning only, while `DECOMPOSE.md:253-264` enumerates the planning deliverables.
- The tracked implementation/tooling file `mise.toml:26-28` has already been changed from task `fe:typecheck` to `fe:check` during synthesis.
- The plan still says US-002 will replace the **current** `fe:typecheck` task at `docs/stories/US-002-snapshot-v1-contract.md:19` and `docs/TODO.md:12`, but that current task no longer exists.
- R1-01 requested moving ownership into US-002's plan, not performing US-002 implementation during planning (`.planning/REVIEW-1.md:25-27`).

**Impact**

The planning exercise has mutated a non-deliverable implementation/tooling file and made its own US-002 scope factually stale. US-002 would begin with its named rename already completed outside the required Worker/Reviewer story workflow.

**Minimal resolution**

Revert only the `mise.toml` task-name change to `fe:typecheck`. Keep the planned rename in US-002 so its future Worker performs and validates it under the mandated story workflow.

### R2-02 — Medium — Moving task ownership to US-002 introduced a hidden frontend dependency

**Evidence**

- US-002 owns creation of the mandated frontend validation task at `docs/stories/US-002-snapshot-v1-contract.md:19,47`.
- US-008 acceptance criterion 6 says it reuses tasks introduced by US-002 at `docs/stories/US-008-offline-pwa-shell.md:47`.
- US-008 nevertheless declares no dependency at `docs/stories/US-008-offline-pwa-shell.md:24-26`; only TODO placement (`docs/TODO.md:19,25`) currently supplies the hidden prerequisite.
- US-009 still lists both US-002 and US-008 at `docs/stories/US-009-local-snapshot-storage.md:24-27`, and US-013 does the same at `docs/stories/US-013-safe-post-markup.md:24-27`.

**Impact**

After R2-01 restores the planning baseline, US-008 is not independently implementable from its declared prerequisites because its required validation task comes from US-002. Adding the missing edge without reducing downstream edges would then make US-002 transitive in US-009 and US-013.

**Minimal resolution**

Add direct dependency US-002 to US-008. Then remove US-002 from US-009 and US-013 because both receive it transitively through US-008. Re-run the graph reduction after these three edits.

### R2-03 — Medium — US-016's new US-006 dependency is transitive

**Evidence**

- US-016 lists US-006 and US-011 at `docs/stories/US-016-first-party-images.md:26,28`.
- US-011 depends on US-010 at `docs/stories/US-011-client-refresh-jitter.md:23-25`, and US-010 depends on US-006 at `docs/stories/US-010-client-lineage-sync.md:25-28`.
- US-016 also reaches US-006 through US-015 → US-014 → US-012 → US-010.
- `.planning/TRACEABILITY.md:236` and `.planning/AGENT_REPORT.md:37` claim that only direct prerequisites remain.

**Impact**

The graph is topologically ordered but not transitively reduced, so the exact dependency claim and the R1-03 resolution are false.

**Minimal resolution**

Remove US-006 from US-016. Its direct branch leaves are US-007, US-011, and US-015; those retain both backend branches and the independent frontend scheduling/rendering branches.

### R2-04 — Medium — Final traceability still maps to the temporary architecture draft

**Evidence**

- `.planning/TRACEABILITY.md:5-8` now correctly declares only final artifacts and resolved interpretations as proof.
- The `Axioms` row at `.planning/TRACEABILITY.md:49` still maps to undefined “architecture `Axioms`, `Derived invariants`,” terms belonging to the temporary architecture analysis rather than a final deliverable.
- `.planning/TRACEABILITY.md:176` likewise maps `Technology Rationale / Philosophy` to “Architecture classification.”
- `DECOMPOSE.md:67-75` makes specialist planning artifacts temporary and reserves final deliverables to the Coordinator.

**Impact**

The matrix's method and its rows disagree. Two coverage claims still depend on a non-final artifact that future readers need not retain or trust.

**Minimal resolution**

Remove the two architecture-draft references. Map the Axioms row only to final MADRs, final stories, TODO guardrails, and I2–I8; map the technology-philosophy row to final stories/TODO assumptions or state that it is a non-implementing rationale covered by the global guardrail.

### R2-05 — Low — The locally scoped port resolution was not propagated exactly

**Evidence**

- `docs/TODO.md:11` correctly limits 65100–65199 to project-controlled local/Compose listeners, proxy targets, container, health-check, and published ports and excludes remote destinations.
- `.planning/TRACEABILITY.md:229` reverts to the broader wording “Every service/listener/container port” and omits the remote-destination exclusion.
- `.planning/AGENT_REPORT.md:28` also summarizes the resolution as “every listener, target, container, health-check, and published port” without the project-controlled/local qualification.

**Impact**

The same resolved assumption has different scopes across final artifacts, allowing the external-port ambiguity from R1-06 to reappear when traceability or the Agent Report is read alone.

**Minimal resolution**

Use the exact locally scoped TODO wording, or an equivalent unambiguous abbreviation, in `.planning/TRACEABILITY.md` and `.planning/AGENT_REPORT.md`.

### R2-06 — Low — The Agent Report contains stale resolution claims

**Evidence**

- `.planning/AGENT_REPORT.md:31` says US-008 owns the `fe:check` rename, while the current plan and the later report row at `.planning/AGENT_REPORT.md:45` assign it to US-002.
- `.planning/AGENT_REPORT.md:37` says final stories list only direct prerequisites, contradicted by R2-03 and, after the required R2-02 edge is added, the current US-009/US-013 lists.
- `.planning/AGENT_REPORT.md:10,55` correctly identify round 2 as pending at review start, so those lines need normal completion bookkeeping after this report rather than removal now.

**Impact**

The required record of findings and resolutions is not internally current and gives conflicting implementation ownership/dependency conclusions.

**Minimal resolution**

Mark the US-008 ownership row as superseded by the round-1 fix or update it to US-002. After R2-02/R2-03 are resolved, refresh the direct-dependency conclusion and add the round-2 report, findings, and resolutions.

## Regression Checks With No Additional Finding

- All five MADRs remain cohesive, decision-worthy, one-decision records with no lifecycle/status fields or duplicated decision.
- No complete User Story duplicates another; acceptance/validation remains limited to unit and integration tests, and TODO is the only completion-state artifact with every box unchecked.
- Same-ID browser staging/activation, local generation keys, atomic replacement, and failure preservation remain consistent across MADR 0002, US-009, US-010, TODO, traceability, and open questions.
- Request/retry/degradation defaults remain explicit and do not change activation semantics; deadline expiration and external/shutdown cancellation are now correctly distinct.
- HTTP-root reuse and Collector wiring-versus-pipeline ownership are clear and non-duplicative.
- TODO order remains topologically valid, locked exclusions remain excluded, and no seed requirement became unmapped through the round-1 edits.
