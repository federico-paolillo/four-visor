# Agent Instructions

## Authority and Durable Artifacts

- `docs/SEED.md` is the authoritative product and architecture specification.
- MADRs, User Stories, `docs/TODO.md`, and traceability reports are generated
  planning or process artifacts. They must agree with the SEED and must not
  silently override it.
- Use `.agents/log/` for progress summaries, `.agents/planning/` for temporary
  planning and traceability artifacts, `docs/TODO.md` for implementation state,
  `docs/madrs/` for MADRs, and `docs/stories/` for User Stories.
- When a requested correction changes a locked requirement, route, default,
  delivery contract, or completed behavior, update the durable SEED first.
  Then rerun or amend decomposition so the generated MADRs, stories,
  `docs/TODO.md` assumptions, and traceability agree before implementation
  begins. Do not implement a specification reversal against stale generated
  artifacts.
- Implementation choices left open by the SEED and the selected User Story may
  be resolved within the workflow below.

## User Story Implementation Workflow

When implementing one User Story:

1. Always stay on branch `main`.
2. Spawn a high-effort Worker via Herdr, ask it to plan the selected User Story,
   and approve the plan before authorizing implementation.
3. Authorize the same Worker to implement and validate the approved plan.
4. When the Worker reports completion, spawn a high-effort Reviewer via Herdr.
5. If the Reviewer reports findings, wake the same Worker and ask it to fix
   them.
6. Repeat the Worker-Reviewer loop until the Reviewer is satisfied or five
   review loops have completed.
7. Stop after five review loops even if findings remain. Record all remaining
   findings in [`DEBT.md`](./DEBT.md) and report them clearly. Do not mark the
   story done unless the Reviewer approved it.
8. Mark an approved User Story as done by checking its box in `docs/TODO.md`.
9. Commit and push one commit using the User Story title as the message.

The Worker owns implementation. The Reviewer owns independent verification.
The Coordinator owns orchestration and final workflow bookkeeping. Review is
required before a User Story is complete.

## Guidelines for Coordinators

- Coordinate the workflow; do not take over implementation from the Worker or
  verification from the Reviewer.
- Within the approved User Story and SEED authority, autonomously approve or
  reject plans, authorize implementation, route review findings, and continue
  the workflow without routine user confirmation.
- Stop and request direction when progress requires changing the SEED,
  resolving a contradiction in durable inputs, expanding scope materially, or
  obtaining authority the workflow does not grant.
- Give each agent its complete task, constraints, and expected handoff marker up
  front, then leave it undisturbed while Herdr reports it as working.
- Wait at least 20 uninterrupted minutes after assigning work or receiving the
  agent's last message before sending a follow-up, interrupt, status request, or
  nudge. Passive Herdr status and pane reads are allowed.
- The 20-minute quiet period may end early only when Herdr reports a blocker
  such as `blocked`, `waiting`, or `error`; the terminal is visibly awaiting
  input; the agent asks a question; or the user redirects the task.
- After 20 minutes without a handoff, check Herdr. Send at most one concise
  status request, or explicitly grant another 20-minute quiet period. Never
  interrupt merely because no output or file change is visible.
- Use these handoff markers, agreed in the initial task: `PLAN READY` for plan
  approval, `WORK READY` for validated implementation, `REVIEW READY` for review
  results, and `BLOCKED: <reason>` when Coordinator input is required.
- Treat the marker in the pane transcript as the handoff source of truth.
  Herdr's `done` state is only an unseen-completion notification, and a
  completed interactive agent may report `idle`; on `idle`, `done`, or
  `unknown`, inspect recent unwrapped output before waiting or sending a status
  request.
- Do not authorize implementation before approving the Worker's plan. Give
  feedback at handoffs and tie it to requirements, blockers, validation, or
  review findings instead of prescribing moment-to-moment work.
- Cap each phase at five rounds: five plan rounds, five work rounds, and five
  review rounds. A round starts when the Coordinator assigns or returns that
  phase and ends at its handoff marker.
- If a cap is reached, stop that phase and report unresolved work. Record
  remaining implementation or review findings in `DEBT.md`; an unapproved story
  remains incomplete.
- After approval, the Coordinator alone updates `docs/TODO.md`, creates and
  pushes the single required commit, and closes the Herdr panes.
- Once the job is complete, produce a Session Overview using the template below.

### `/goal` Progress Summaries

These instructions apply only when the Coordinator is running a long-horizon
task through `/goal`. They do not apply to ordinary User Story execution or
other short-lived work.

- At the start of the `/goal` run, generate a stable opaque identifier for that
  goal session. Reuse the same identifier in every progress summary produced by
  that run. The identifier exists only to correlate summaries from the same
  `/goal` session and must not encode user, repository, branch, or goal details.
- Emit the first progress summary after the first milestone; do not create an
  initial zero-progress summary.
- Prefer milestone boundaries based on completed atomic tasks. Emit a summary
  whenever one such task completes. When the work has no useful atomic task
  units, use the Coordinator's best judgment to emit at roughly 10% progress
  increments.
- Emit a summary at every applicable milestone even when nothing noteworthy
  happened during the interval. State an uneventful interval briefly.
- Before writing each summary, reread the most recent summary in `.agents/log/`
  carrying the same goal-session identifier. Summarize only events occurring
  after that file. If no prior matching summary exists, summarize events since
  the `/goal` run began. Do not treat summaries from another identifier as
  prior context.
- Capture the major plot points needed to understand the interval: work
  completed, material discoveries, decisions, changes of direction, review or
  validation outcomes, blockers, errors, interruptions, retries, and notable
  Worker or Reviewer handoffs. Include both events directly observed by the
  Coordinator and relevant events reported by delegated agents. Keep the
  account brief and do not repeat details already covered by the previous
  matching summary.
- Include the stable goal-session identifier in the file so later emissions can
  reliably locate the previous matching summary. No other fixed metadata schema
  is required.
- Write each summary as a new Markdown file under `.agents/log/`, creating that
  directory when necessary. Use a UTC filename in the form
  `yyyy-mm-dd-hh-mm-ss.md`, for example `2026-07-27-14-32-08.md`. If that name
  already exists, append a two-digit suffix such as `-01` rather than
  overwriting a file.
- Treat these summaries as repository artifacts: include them in the final
  commit and push. They do not replace the final Session Overview; produce both.
- If a progress summary cannot be written, continue the `/goal` workflow and
  report the failure clearly at the next available handoff and in the final
  Session Overview.

## Shared Engineering Guidelines

> The word "module" means a cohesive unit of code in the relevant language or
> framework.

- Prefer established language, framework, and standard-library capabilities
  over custom implementations. Use modern, idiomatic syntax.
- Apply SOLID and GRASP where they make responsibilities clearer. A little
  ceremony is welcome when it visibly produces smaller, more cohesive modules.
- Keep one module per file unless a module is private or used only by the code
  declaring it. Do not split cohesive models or private helpers solely by size.
- Keep endpoint handlers, service wiring, dependency checks, and
  implementations in concern-specific files or folders; keep composition roots
  small.
- Prefer guard clauses, descriptive domain names, and named conditions over
  dense control flow. Centralize genuinely repeated rules, constants, encoding,
  and validation in small intention-revealing helpers. Keep sibling paths
  behaviorally consistent.
- Refactor when it is necessary to implement or review the story cleanly. Avoid
  unrelated cleanup unless it is required for canonical validation to pass.
- At dependency boundaries, distinguish invalid data from unavailability,
  propagate cancellation, preserve exception causes, keep diagnostics
  secret-free, and cover changed failure behavior with focused tests.
- Give each filtering, normalization, validation, and configuration policy one
  authoritative enforcement owner. Add another enforcement layer only for a
  distinct trust boundary or independently stated failure mode.
- For telemetry, application code owns secret filtering, cardinality, and event
  semantics; telemetry infrastructure owns transport, routing, batching,
  authentication, and sampling. Do not duplicate application field allowlists
  downstream without a distinct trust-boundary requirement.
- This is not an enterprise-grade workflow. Cover the happy path and the most
  obvious and material failure paths; do not harden against every conceivable
  edge case.

## Verification Guidelines

- Before handoff, run the repository's documented canonical aggregate
  validation entrypoint when one exists. Otherwise run all applicable canonical
  Mise tasks from `mise.toml`: `mise run be:build`, `mise run be:test`, and
  `mise run be:lint` for backend changes; `mise run fe:build`,
  `mise run fe:test`, `mise run fe:lint`, and `mise run fe:check` for frontend
  changes. Run both sets when both areas change.
- Do not hand off validated work until all applicable canonical tasks pass
  without errors.
- Unit tests start no external processes. Integration tests may provision
  task-owned dependency containers and must not require a prestarted project
  stack, a fixed shared Compose project, or a fixed host port.
- Automated tests are limited to unit and integration tests. Do not create or
  demand smoke, end-to-end, browser end-to-end, or deployment tests.
- Native configuration rendering, image building, and release-archive assembly
  are build validation, not smoke or deployment tests. Prefer a tool's native
  parser, renderer, compiler, or build command. Add a bespoke structural
  validator only when native tooling cannot prove a required invariant.
- Test each behavior once at the lowest stable boundary that can prove it. Add
  a cross-boundary integration test only when behavior can fail despite the
  lower-level check, such as serialization compatibility, dependency semantics,
  or framework wiring.
- Require negative-path coverage for trust boundaries, security rules,
  cancellation and concurrency, atomic data replacement, data-loss risks, and
  previously observed regressions. Do not require a test for every branch,
  defensive condition, or log call.
- Log prose is not an API. Test exact text only when the wording is itself a
  stable filtering or operator contract. Otherwise test severity, event
  identity, required fields, forbidden sensitive fields, and cardinality with
  one representative event per policy class.
- Do not reproduce the same assertion across unit, integration, image,
  configuration, script, and documentation checks merely for reassurance.
  Reuse an existing test at the owning boundary and delete redundant checks
  when a stronger authoritative check supersedes them.
- Do not introduce a test seam, interface, fake, fixture family, or helper whose
  complexity exceeds the behavior it verifies.
- Test volume, branch count, mutation survival, and assertion count are not
  goals. Validation ends when the story's observable acceptance criteria and
  material regression risks are covered.

## Documentation and Comment Guidelines

- First make production code explain itself through precise names, explicit
  types, cohesive structure, ordinary control flow, and established idioms.
- Add a concise inline comment only when important reasoning cannot be recovered
  quickly from the code: a non-obvious invariant, security or compatibility
  constraint, protocol ownership, calibrated limit, or why a simpler-looking
  implementation is incorrect.
- Do not require file, module, function, or test comments merely for coverage.
  Do not narrate names or create a Markdown document instead of clarifying the
  code. Prefer a precise comment beside the constrained mechanism.
- Keep README content to project purpose, expected environment variables and
  their contract, and non-obvious operator constraints. Do not teach standard
  Docker, Compose, shell, Git, or service-lifecycle commands, and do not narrate
  implementation details.
- SEED, MADRs, stories, TODO, and traceability files are specification or
  process artifacts, not product documentation. Do not create separate feature,
  developer, implementation, test, or architecture documents unless the SEED
  explicitly requires one. Encode behavior in code and focused tests; explain
  remaining non-obvious reasoning inline.

## Runtime and Container Safety

- Use ports in the range 65100-65199 for Docker Compose, Docker containers, and
  services in general.
- Bind every published Docker or Compose port explicitly to `127.0.0.1`.
- Test-only containers may use dependency-native container ports and
  Docker-assigned ephemeral host ports, but every published test port must bind
  explicitly to `127.0.0.1`.
- Never require `sudo`, privileged containers, firewall changes, or a shared
  prestarted dependency.
- Harden images made by the project as rootless and distroless with read-only
  filesystems. Do not impose project-owned hardening on third-party images.

## Guidelines for Workers

- Implement only the approved plan and changes needed to satisfy the selected
  User Story.
- Follow the shared engineering, verification, documentation, and safety
  guidelines above.
- Before `WORK READY`, run applicable validation and report the commands and
  results. Use `BLOCKED: <reason>` instead when validated implementation cannot
  be completed.
- If an issue persists between review rounds and further changes are circling,
  choose the simplest acceptable solution that passes validation, record any
  material shortcoming in `DEBT.md`, and move on. Do not claim completion if
  unresolved work prevents Reviewer approval.

## Guidelines for Reviewers

- Independently verify every requirement and acceptance criterion of the User
  Story.
- Follow the shared engineering, verification, documentation, and safety
  guidelines above when evaluating the implementation.
- Report significant security issues, inconsistent sibling behavior, vague
  names, dense control flow, mixed independent concerns, incorrect failure
  classification, lost cancellation or exception causes, secret-bearing
  diagnostics, and missing material negative paths.
- Evaluate whether framework, standard-library, and native-tool capabilities
  were used or considered before custom code or validation was added.
- Tie every finding to an unmet acceptance criterion, a material regression
  risk, or a concrete maintainability problem. Untested code alone is not a
  finding.
- Reject duplicate policy enforcement and redundant tests unless each protects
  a distinct trust boundary or failure mode. Prefer deletion or consolidation
  when an authoritative check makes an older helper, fixture, script, or test
  redundant.
- Do not flag anything already documented in `DEBT.md`.
- Finish with `REVIEW READY`, including the validation performed, findings, and
  whether the User Story is approved. Use `BLOCKED: <reason>` if verification
  cannot be completed.

## Session Overview

Use this template to produce a Session Overview. Omit parts that do not make
sense for the session.

```markdown
Session Summary:

User Story: US-XYZ "Title"

- Worker-Review loops: 2
- Review findings: 28
- DEBT.md changes: 0
- Status requests sent: 2
- 20-min window extensions: 2
- Agent interruptions: 1
- Ran to completion: yes

This optional paragraph briefly reports errors, interruptions, restarts, or
other events that prevented full completion or required human intervention.
```
