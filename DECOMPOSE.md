# Decompose Project Planning Goal

Decompose the project defined in `docs/SEED.md` into a complete implementation
plan consisting of:

- Markdown Architectural Decision Records (MADRs) under `docs/madrs/`
- User Stories under `docs/stories/`
- A sequential implementation checklist in `docs/TODO.md`

This is a planning exercise only. Do **not** implement production code.

# Inputs and authority

Use `docs/SEED.md` as the authoritative product specification. Read it in full
before decomposition.

Read and follow `AGENTS.md`. Apply its coordination model, handoff conventions,
and applicable iteration limits, but ignore language- or framework-specific
implementation guidance that is not relevant to planning. Do not duplicate
those coordination rules here.

If planning artifacts already exist, update them instead of duplicating them.
Generated MADRs, stories, TODO entries, assumptions, and traceability must not
override or silently amend the SEED.

Every normative SEED requirement must have a unique, stable identifier. Treat a
missing or duplicate identifier as a blocking input defect: record it in
`.agents/planning/OPEN_QUESTIONS.md` and do not invent a replacement that could
be mistaken for product authority.

# Coordinator responsibilities

Act only as the Coordinator.

Coordinate specialists, reconcile their outputs, run the decomposition
protocol, and produce the final planning artifacts. Do **not** perform the
complete decomposition yourself or delegate authority over the final files.

The Coordinator alone records the acceptance-gate outcome after independent
review and must not approve the plan while a blocking review finding remains.

# Agent strategy

Partition the work to minimize context pollution. Use multiple independent
agents with narrowly scoped responsibilities.

At minimum create:

1. **Architecture Agent**
   - Extract architectural decisions.
   - Distinguish axioms, locked decisions, unresolved decisions, requirements,
     exclusions, and implementation details.
   - Identify where MADRs are warranted.
   - Flag decisions that require feasibility evidence.
2. **Story Agent**
   - Decompose the SEED into independently completable User Stories.
   - Assign requirement and lifecycle ownership.
   - Produce explicit dependency relationships.
3. **Traceability Agent**
   - Map every normative requirement to its first-owner story, exact acceptance
     criterion, canonical validation, and downstream consumers.
   - Detect omissions, duplicate ownership, unsupported assumptions, and
     conflicts.
4. **Review Agent**
   - Remain independent from architecture extraction and story synthesis.
   - Review feasibility, invariants, operability, dependency ordering,
     traceability, validation boundaries, and unsupported assumptions.

Additional specialist agents may be created when a distinct concern warrants
one. Do not create agents merely to repeat another agent's checks.

Give each agent only the context required for its responsibility. Agents must
not edit final deliverables directly. Each agent writes findings into temporary
planning artifacts under:

```text
.agents/planning/
```

The Coordinator is solely responsible for producing the final files.

# Decomposition protocol

Complete these phases in order. A later phase may return the work to an earlier
phase when it exposes a conflict.

## Phase 1: Requirement inventory and template preflight

Before creating MADRs or stories:

1. Inventory every normative requirement identifier and classify the statement
   as an axiom, locked decision, open decision, requirement, exclusion, or
   explanatory material.
2. Detect missing or duplicate identifiers, contradictory values, incompatible
   requirements, repeated normative declarations, and conflicts between
   requirements and exclusions.
3. Compare the SEED with the actual template, canonical tasks, configuration,
   documentation paths, existing defaults, and existing planning artifacts.
4. Run the clean-checkout validation entrypoint advertised by the template.
5. Record broken prerequisites and contradictions as blocking open questions.

Do not assume a broken prerequisite will be repaired by a later story. If the
SEED authorizes its repair, make that repair the first story required by every
consumer. Otherwise stop the affected planning path as blocked.

## Phase 2: Quantitative closure and feasibility

Identify every requirement whose correctness depends on quantities or budgets.
For each applicable relationship, calculate or measure:

- workload volume against rate, concurrency, retry, timeout, deadline, and
  cadence limits;
- simultaneous live, staged, replacement, rollback, and cleanup states against
  storage or cache capacity plus explicit overhead;
- representative payload size through every producer, intermediary, transport,
  consumer, and persistence boundary;
- encoded, transferred, decoded, persisted, and peak working-memory sizes as
  distinct values;
- long-running operation duration against proxy, client, job, telemetry,
  sampling, export, and retention windows; and
- any other producer-consumer budget in which a downstream limit can invalidate
  an upstream operation.

Record the formula, inputs, provenance, command or measurement method,
assumptions, headroom, and conclusion. A label such as "large," "fast," or
"sufficient" is not feasibility evidence.

Do not invent a value merely to eliminate an open question. When an unknown can
be measured, create the earliest possible calibration or characterization story
and make dependent decisions and stories wait for its evidence. If no authorized
story can obtain the evidence, keep the question blocking.

Calibration is implementation evidence, not automatically a permanent test.
Add a lasting automated check only when it protects an acceptance criterion or
material regression risk.

## Phase 3: Story ownership and dependency planning

Assign each normative requirement to the story that first makes it true. That
story owns all required behavior originating at its boundary, including success,
failure classification, cancellation, security, observability, configuration,
cleanup, and non-obvious operator information.

Do not defer missing semantics to a final cleanup, hardening, observability, or
integration story. A later integration story may prove an end-to-end path, but
it must not be the first owner of behavior required by an earlier story.

For every dependency, name the predecessor story and the decision, evidence,
invariant, interface, or artifact consumed. Build an acyclic order in which:

- prerequisite and calibration stories precede their consumers;
- stories that establish a contract precede stories that rely on it;
- no story depends on a later TODO entry; and
- each implementation story leaves its slice usable and operable within the
  scope reached so far.

Prefer vertical slices over technical layers when practical. Split a story only
when the resulting stories remain independently implementable and do not hide
lifecycle ownership between them.

## Phase 4: Acceptance-level traceability

Section-level or story-level traceability is insufficient. For every normative
requirement, identify:

- the first-owner User Story;
- the exact acceptance-criterion identifier or identifiers that prove the
  requirement;
- the canonical validation mechanism and owning boundary;
- any MADR that resolves a genuinely open decision;
- every downstream story that consumes the invariant;
- the feasibility evidence or explicit blocking question, when applicable; and
- whether an operator needs non-obvious configuration or constraint information.

Use justified `N/A` entries for genuinely inapplicable columns. Do not use `N/A`
to conceal an omission.

## Phase 5: Cross-story invariant review

After ordering the stories, trace every applicable invariant from its producer
through all transformations, transports, persistence boundaries, and consumers.
At minimum consider chains of these forms:

```text
workload -> rate and concurrency -> retries and timeouts -> deadline -> cadence
data size -> representation -> transport hops -> decode -> persistence -> reads
active state -> staged replacement -> activation or rollback -> cleanup -> capacity
operation lifetime -> telemetry creation -> export or sampling -> retention
failure boundary -> controlled classification -> diagnostic signals -> operator action
build identity -> produced artifacts -> configuration -> release -> deployment input
```

Use only chains applicable to the SEED. Add project-specific chains when needed,
but do not omit a consumer merely because it is implemented in another story or
layer.

No story set passes while a downstream consumer imposes a smaller budget,
different value, incompatible contract, or weaker lifecycle guarantee than the
operation it consumes.

## Phase 6: Semantic validation and delivery boundaries

Review what each proposed validation does rather than what it is called.

- Map every acceptance criterion to the lowest stable boundary that can prove
  it.
- Use a cross-boundary integration check only for behavior that can fail despite
  the lower-level check, such as serialization, dependency semantics, or
  framework wiring.
- Keep integration dependencies task-owned and isolated; do not require a
  shared, prestarted project environment unless the SEED explicitly requires
  one.
- Treat native compilation, parsing, configuration rendering, image building,
  and artifact assembly as build validation when that is what they perform.
- Prefer a native parser, compiler, formatter, or build command over a bespoke
  structural validator.
- Do not relabel a complete deployment probe or browser-spanning workflow as an
  integration test. Generate only validation categories permitted by the SEED.
- Do not duplicate the same rule at multiple layers unless each check names a
  distinct trust boundary or failure mode unavailable at the other boundary.
- Give each filtering, normalization, validation, and authorization policy one
  authoritative enforcement owner. Add another owner only for a distinct trust
  boundary or independently required failure mode.
- Test diagnostic event identity, severity, required fields, forbidden fields,
  and cardinality. Test exact prose only when the wording is itself a stable
  contract.
- Require negative-path validation for trust boundaries, security rules,
  cancellation, concurrency, atomic replacement, data-loss risks, and observed
  regressions; do not require a test solely for every branch or line.
- Delete or consolidate a helper, fixture, script, or test made redundant by a
  stronger authoritative check.

Generate CI, release, packaging, and deployment-consumption work only when the
SEED requires it. When delivery is required, trace one build identity through
all produced artifacts and deployment inputs. When it is excluded, do not add
it through an assumption or template default.

Product documentation is limited to the documentation contract in the SEED.
Prefer clear code and focused inline explanations for non-obvious reasoning.
Require operator documentation only for environment contracts or non-obvious
operational constraints; do not create documentation that teaches ordinary tool
usage or narrates implementation details.

## Phase 7: Operability review

Before finalizing, the independent Review Agent must answer, where applicable:

1. Can the representative production workload finish within every linked
   budget with stated headroom?
2. Can active, incoming, rollback, and cleanup states coexist within capacity?
3. Can representative data traverse every required boundary without violating
   size, time, memory, or compatibility constraints?
4. Can important failures be diagnosed without secrets or unbounded
   cardinality?
5. Can a clean checkout run every canonical validation task without a shared
   prestarted environment?
6. Are CI, release, packaging, and deployment consumption intentionally present
   or absent according to the SEED?
7. Do MADRs contain only genuine decisions left open by the SEED?
8. Does every story introduce a complete and currently operable slice?
9. Is each important rule implemented and validated at one named owning
   boundary?
10. Is operator information limited to required environment contracts and
    non-obvious operational constraints?

Record a justified `N/A` for an inapplicable question. Any unsupported "yes" is
a review finding.

# MADRs

Create MADRs under:

```text
docs/madrs/
```

Use sequential numbering:

```text
0001-title.md
0002-title.md
...
```

Each MADR should contain:

- Title
- Context
- Decision
- Decision Drivers
- Considered Options
- Consequences
  - Positive
  - Negative
- Related User Stories
- Traceability to stable requirement identifiers in `docs/SEED.md`

Rules:

- Do not introduce lifecycle or workflow status fields.
- Create a MADR only for a meaningful architectural decision left open by the
  SEED.
- Do not create MADRs that restate axioms, requirements, exclusions, or locked
  decisions.
- When a decision depends on quantitative claims, include or reference the
  required feasibility evidence.
- Keep one architectural decision per MADR.

# User Stories

Create one Markdown file per User Story under:

```text
docs/stories/
```

Name them:

```text
US-001-short-title.md
US-002-short-title.md
...
```

Each story must contain:

- ID
- Title
- Goal
- User Value
- Scope
- Out of Scope
- Dependencies
- Related MADRs
- Traceability to stable requirement identifiers in `docs/SEED.md`
- Acceptance Criteria
- Validation

Give every acceptance criterion a stable story-scoped identifier, such as
`US-001-AC-01`. Map every identifier to its canonical validation mechanism.

Rules:

- Do not introduce workflow or lifecycle status fields into User Stories.
- Story completion is tracked exclusively in `docs/TODO.md`.
- A story describes work to implement, not its implementation state.
- Dependencies must name predecessor story IDs and what each dependency
  provides.
- Traceability must name exact requirement and acceptance-criterion IDs.
- Scope and acceptance criteria must include the complete required lifecycle at
  the boundary the story first owns.

Stories should:

- be independently implementable and reviewable;
- have objective acceptance criteria that fail when the behavior regresses;
- produce observable progress;
- minimize coupling;
- be ordered by dependency; and
- avoid hidden prerequisites.

Prefer vertical slices over technical layers whenever practical.

Do not create placeholder stories such as:

- Implement a layer
- Add tests
- Improve logging
- Final cleanup

Integrate testing, observability, documentation, security, failure handling,
cancellation, and cleanup into the stories where those behaviors originate.

# TODO

Generate:

```text
docs/TODO.md
```

The TODO is the implementation sequence and the **only** place that tracks
implementation progress.

Format:

```markdown
# Implementation Plan

## Planning assumptions

...

## User Stories

- [ ] US-001 Title
- [ ] US-002 Title
- [ ] US-003 Title
```

The order must be acyclic and reflect implementation dependencies. Calibration,
prerequisite, and contract-defining stories must precede their consumers. No
story may depend on a later story. Do not check any boxes.

Planning assumptions must reference the relevant requirement or open-question
identifier. Do not copy normative values into TODO when a requirement reference
is sufficient, and do not use an assumption to contradict or extend the SEED.

# Traceability

Generate:

```text
.agents/planning/TRACEABILITY.md
```

Create one entry for every normative requirement with:

- requirement identifier;
- first-owner User Story;
- exact acceptance-criterion identifier or identifiers;
- canonical validation and owning boundary;
- related MADR or explicit assumption;
- downstream consuming stories;
- feasibility evidence or blocking question, when applicable; and
- required operator information, or a justified `N/A`.

Also record unmapped requirements, duplicate ownership, conflicts, and
unsupported claims. Do not mark an entry complete merely because it references
a story without an exact acceptance criterion and validation.

# Review and acceptance gate

After synthesis, use the independent Review Agent. The reviewer must verify:

- complete acceptance-level coverage of every normative SEED requirement;
- no contradiction with a SEED value, exclusion, or locked decision;
- no duplicated decisions, stories, lifecycle ownership, or enforcement without
  a distinct boundary;
- quantitative closure or an explicit earlier calibration dependency;
- cross-layer invariant consistency through every consumer;
- correct, acyclic dependency ordering with no hidden prerequisites;
- independently implementable and operable stories;
- objective acceptance criteria and semantically correct validation;
- consistency between MADRs, stories, TODO, traceability, and assumptions;
- no invented constants, requirements, production behavior, or documentation;
  and
- intentional inclusion or exclusion of CI, release, packaging, and deployment
  work according to the SEED.

Resolve findings before finalizing whenever possible. Re-run every affected
protocol phase after a resolution; do not patch only the final artifact.

Record unresolved issues in:

```text
.agents/planning/OPEN_QUESTIONS.md
```

Classify each open question as blocking or non-blocking, identify affected
requirements and stories, state the evidence needed to resolve it, and avoid
inventing an answer merely to reach zero open questions.

## Stopping rules

Stop adding validation when each acceptance criterion and material regression
risk is proved once at the lowest stable boundary. Test volume, assertion count,
branch count, and zero open questions are not goals.

The decomposition is approved only when the independent reviewer confirms that:

- every normative requirement has acceptance-level traceability;
- no blocking conflict or unsupported feasibility claim remains;
- all cross-layer invariants and dependencies are consistent;
- every affected story is directly implementable through the workflow in
  `AGENTS.md`; and
- all generated artifacts agree with the SEED and with one another.

Honor the applicable phase and review-round limits in `AGENTS.md`. If a limit is
reached, stop that phase, record unresolved work in `OPEN_QUESTIONS.md`, and
report the plan as incomplete. Do not call the plan approved or name a blocked
story as ready for implementation.

# Deliverables

Produce:

```text
docs/madrs/
docs/stories/
docs/TODO.md
.agents/planning/TRACEABILITY.md
.agents/planning/AGENT_REPORT.md
.agents/planning/OPEN_QUESTIONS.md
```

`AGENT_REPORT.md` must record:

- agents used and each responsibility;
- temporary artifacts produced;
- preflight and feasibility results;
- conflicts and lifecycle-ownership decisions;
- review findings and resolutions;
- review rounds and stopping condition; and
- remaining open questions and blockers.

# Constraints

- Do not modify application source code.
- Do not create implementation work beyond the SEED.
- Preserve the distinction between axioms, requirements, architectural
  decisions, exclusions, assumptions, and implementation details.
- Do not introduce lifecycle or completion state into MADRs or User Stories.
- Completion state exists only in `docs/TODO.md`.
- Prefer explicit assumptions over hidden assumptions, but never use an
  assumption to replace product authority.
- Keep policies and their validation at one authoritative owner unless a
  distinct trust boundary or failure mode requires another.
- Every User Story must be directly implementable through the existing
  Worker/Reviewer workflow defined in `AGENTS.md`.

At completion, report:

- Number of MADRs created.
- Number of User Stories created.
- Number of open questions, separated into blocking and non-blocking.
- Quantitative feasibility and acceptance-gate status.
- The first unblocked User Story ready for implementation, if any.
- Every requirement that could not be mapped or validated confidently.
