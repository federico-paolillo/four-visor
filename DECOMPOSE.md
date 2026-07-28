# Decompose Project Planning Goal

Decompose the project defined in `docs/SEED.md` into a complete implementation
plan consisting of:

- Markdown Architectural Decision Records (MADRs) under `docs/madrs/`
- User Stories under `docs/stories/`
- A sequential implementation checklist in `docs/TODO.md`

This is a planning exercise only. Do **not** implement any production code.

# Inputs

Use `docs/SEED.md` as the authoritative product specification.

Read and follow `AGENTS.md`. Apply its coordination model (Coordinator →
Worker(s) → Reviewer), but ignore any language- or framework-specific
implementation guidance that is not relevant to planning.

If planning artifacts already exist, update them instead of duplicating them.

# Coordinator responsibilities

Act only as the Coordinator.

Your responsibility is to coordinate specialists, reconcile their outputs, and
produce the final planning artifacts.

Do **not** perform the complete decomposition yourself.

# Agent strategy

Partition the work to minimize context pollution.

Use multiple independent agents with narrowly scoped responsibilities.

At minimum create:

1. **Architecture Agent**
   - Extract architectural decisions.
   - Distinguish between:
     - axioms
     - accepted decisions
     - unresolved decisions
     - implementation details
   - Identify where MADRs are warranted.
2. **Story Agent**
   - Decompose the seed into implementation User Stories.
   - Produce dependency relationships.
   - Ensure every story is independently completable.
3. **Traceability Agent**
   - Verify every in-scope requirement in `docs/SEED.md` maps to one or more:
     - MADRs
     - User Stories
     - documented assumptions
   - Detect omissions and duplicate work.
4. **Review Agent**
   - Independently review the synthesized result.
   - Report missing decisions, dependency mistakes, traceability gaps,
     duplicated stories, oversized stories, and unsupported assumptions.

Additional specialist agents may be created if useful.

Each agent must receive only the subset of context required for its
responsibility.

Agents must not edit final deliverables directly.

Instead, each agent writes its findings into temporary planning artifacts under:

```text
.agents/planning/
```

The Coordinator is solely responsible for producing the final files.

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
- Traceability back to `docs/SEED.md`

Rules:

- Do not introduce lifecycle or workflow status fields (such as Proposed,
  Accepted, Draft, etc.). The repository intentionally does not track decision
  status in MADRs.
- Create a MADR only when there is a meaningful architectural decision to
  document.
- Do not create MADRs that merely restate requirements, axioms, or locked
  decisions.
- When the seed leaves a decision intentionally unresolved, document the chosen
  direction and its rationale in the MADR rather than introducing a decision
  status.
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
- Traceability back to `docs/SEED.md`
- Acceptance Criteria
- Validation

Rules:

- Do not introduce workflow or lifecycle status fields into User Stories.
- Story completion is tracked exclusively in `docs/TODO.md`.
- A story describes the work to be implemented, not its implementation state.

Stories should:

- be independently implementable
- be independently reviewable
- have objective acceptance criteria
- produce observable progress
- minimize coupling
- be ordered by dependency
- avoid hidden prerequisites

Prefer vertical slices over technical layers whenever practical.

Do not create placeholder stories like:

- Implement backend
- Add tests
- Improve logging
- Final cleanup

Instead, integrate testing, observability, documentation, security, and failure
handling into the stories where they belong.

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

The order must reflect implementation dependencies.

No story should depend on a later story.

Do not check any boxes.

# Traceability

Generate:

```text
.agents/planning/TRACEABILITY.md
```

Map every relevant section of `docs/SEED.md` to:

- MADRs
- User Stories
- assumptions

Flag anything that cannot be mapped confidently.

# Review

After synthesis, use a separate Review Agent that did not participate in
synthesis.

The reviewer must verify:

- complete coverage of `docs/SEED.md`
- no duplicated decisions
- no duplicated stories
- correct dependency ordering
- independently completable stories
- objective acceptance criteria
- traceability completeness
- consistency between MADRs and stories
- no invented requirements
- no implementation beyond what the seed supports

Resolve reviewer findings before finalizing whenever possible.

Unresolved issues should be recorded in:

```text
.agents/planning/OPEN_QUESTIONS.md
```

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

- agents used
- responsibility of each agent
- artifacts produced
- review findings
- resolutions
- remaining open questions

# Constraints

- Do not modify application source code.
- Do not create implementation tasks beyond the seed.
- Preserve the distinction between axioms, requirements, architectural
  decisions, and out-of-scope functionality.
- Do not introduce lifecycle or completion state into MADRs or User Stories.
- Completion state exists only in `docs/TODO.md`.
- Prefer explicit assumptions over hidden assumptions.
- Every User Story should be directly implementable through the existing
  Worker/Reviewer workflow defined in `AGENTS.md`.

At completion, report:

- Number of MADRs created.
- Number of User Stories created.
- Number of open questions.
- The first User Story ready for implementation.
- Any requirement in `docs/SEED.md` that could not be mapped confidently.
