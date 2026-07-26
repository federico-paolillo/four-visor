# Agent Instructions

## User Story Implementation Workflow

When implementing one user story:

1. Always stay on branch `main`.
2. Spawn a high-effort Worker via Herdr, ask it to plan the selected user story,
   and approve the plan before authorizing implementation.
3. When the Worker reports completion, spawn a high-effort Reviewer via Herdr.
4. If the Reviewer reports findings, wake the same Worker and ask it to fix the
   findings.
5. Repeat the Worker-Reviewer loop until the Reviewer is satisfied or 5 review
   loops have completed.
6. Stop after 5 loops even if findings remain, and report the remaining issues
   clearly.
7. Any remaining findings are to be reported in a DEBT.md file.
8. Mark the completed user story as 'done' in TODO.md by checking the
   corresponding checkbox.
9. Once you are done, commit and push a single commit using the user story title
   as message.

The Worker owns implementation. The Reviewer owns verification. Do not skip
review before considering a user story complete. The agent starting the flow
remains the coordinator and ensures the workflow progresses according to the
instructions.

## Guidelines for Coordinators

- Coordinate the workflow; do not take over implementation from the Worker or
  verification from the Reviewer.
- Give each agent its complete task, constraints, and expected handoff marker up
  front, then leave it undisturbed while Herdr reports it as working.
- Wait at least 20 uninterrupted minutes after assigning work or receiving the
  agent's last message before sending a follow-up, interrupt, status request, or
  nudge. Passive Herdr status and pane reads are allowed.
- The 20-minute quiet period may end early only when Herdr reports a blocker
  such as blocked, waiting, or error; the terminal is visibly awaiting input;
  the agent asks a question; or the user redirects the task.
- After 20 minutes without a handoff, check Herdr. Send at most one concise
  status request, or explicitly grant another 20-minute quiet period. Never
  interrupt merely because no output or file change is visible.
- Use these handoff markers, agreed in the initial task: `PLAN READY` for plan
  approval, `WORK READY` for validated implementation, `REVIEW READY` for review
  results, and `BLOCKED: <reason>` when coordinator input is required.
- Treat the marker in the pane transcript as the handoff source of truth.
  Herdr's `done` state is only an unseen-completion notification, and a
  completed agent may report `idle`; on `idle`, `done`, or `unknown`, inspect
  recent unwrapped output before waiting or sending a status request.
- Do not authorize implementation before approving the Worker's plan. Give
  feedback at handoffs and keep it tied to requirements, blockers, or review
  findings instead of prescribing the agent's moment-to-moment process.
- Cap each phase at 5 rounds: 5 plan rounds, 5 work rounds, and 5 review rounds.
  A round starts when the coordinator assigns or returns that phase and ends at
  its handoff marker.
- If a cap is reached, stop that phase and report the unresolved work. Record
  remaining implementation or review findings in `DEBT.md`, and do not mark the
  story done unless the Reviewer approved it.
- After approval, the coordinator alone performs workflow bookkeeping: update
  `TODO.md`, create the single required commit, push it, and clean up Herdr
  panes.
- Once the job is complete produce a session overview. See "Session Overview"
  section.

### `/goal` Progress Summaries

These instructions apply only when the Coordinator is running a long-horizon
task through `/goal`. They do not apply to ordinary user-story execution or
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
  happened during the interval. An uneventful interval is still useful
  information and should be stated briefly.
- Before writing each summary, re-read the most recent summary in `.log/`
  carrying the same goal-session identifier. Summarize only events occurring
  after that file. If no prior matching summary exists, summarize events since
  the `/goal` run began. Do not treat summaries from another identifier as prior
  context.
- Capture the major plot points needed to understand the interval: work
  completed, material discoveries, decisions, changes of direction, review or
  validation outcomes, blockers, errors, interruptions, retries, and notable
  Worker or Reviewer handoffs. Include both events directly observed by the
  Coordinator and relevant events reported by delegated agents. Keep the account
  brief and do not repeat details already covered by the previous matching
  summary.
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

## Guidelines for Workers

> The word "module" shall be read as a generic denomination of a cohesive unit
> of code in the relevant language or framework.

- Before handoff, run the applicable Mise validation tasks from `mise.toml`:
  `mise run be:build`, `mise run be:test`, and `mise run be:lint` for backend
  changes; `mise run fe:build`, `mise run fe:test`, `mise run fe:lint`, and
  `mise run fe:check` for frontend changes. Run both sets when both areas
  change.
- Do not handoff until all applicable validation tasks pass without errors.
- Make sure to use or evaluate as much as possible framework and standard
  library capabilities before rolling custom implementations.
- Use modern syntax.
- Focus on SOLID, GRASP and idiomatic code.
- Have prejudice over code that is not idiomatic or not standard in the context
  of the language and framework.
- A little verbosity and cerimony are welcome if the contribute visibly to
  smaller files and tidier project structure.
- Be a good citizen. If you find linting errors that were made by someone else
  you can fix them.
- You are allowed to refactor. If you think a section, a class, a method, etc.
  is too large or needs refinement and refactoring do that.
- Try to have one module per file unless the module is private or used strictly
  only within the context of the code declaring it.
- Keep endpoint handlers, service wiring, health checks, and implementations in
  concern-specific files/folders; keep composition roots small.
- Prefer guard clauses, descriptive domain names, and named conditions over
  dense boolean chains or ternaries; comment only non-obvious invariants.
- Centralize genuinely repeated domain rules, constants, encoding, and
  validation in small intention-revealing helpers; keep sibling implementations
  behaviorally consistent.
- At dependency boundaries, distinguish invalid data from unavailability,
  propagate cancellation, preserve exception causes, keep diagnostics
  secret-free, and cover changed failure behavior with focused tests.
- This is not an enterprise-grade project. Do not attempt to harden the software
  against every possible edge-case or conceivable situation. You are allowed (as
  long as you document them) to focus only on the happy path and the most
  obvious failure paths.
- Use ports in range 65100-65199 for Docker Compose, Docker Containers and
  services in general
- Leave one line of comment for each module to explain what it does and why it
  exists.
- Do not write or provide smoke tests. Limit yourself to unit and integration
  tests.
- Do not write or provide end-to-end tests. Limit yourself to unit and
  integration tests.
- Do not write or provide deployment smoke tests. Limit yourself to unit and
  integrations tests.
- Do not harden, rootless, distroless, with read-only fs, any container image
  you did not make.
- Harden or configure as rootless, distroless, with read-only fs, any container
  image you made.
- If an issue persists between review rounds and it looks like you are running
  in circles pick a good enough solution, record any shortcoming to
  [`DEBT.md`](./DEBT.md) and move on.

## Guidelines for Reviewers

> The word "module" shall be read as a generic denomination of a cohesive unit
> of code in the relevant language or framework.

- Ensure all requirements of the user story are met.
- Have prejudice over code that is not idiomatic or not standard in the context
  of the language and framework.
- Ensure that framework and standard library have been used or at least
  evaluated when encountering custom implementations.
- Focus on SOLID, GRASP and idiomatic code.
- A little verbosity and cerimony are welcome if the contribute visibly to
  smaller files and tidier project structure.
- Have a keen security eye and always report significant security issues.
- Point out potential refactoring and refinement areas.
- Flag multiple modules in one file unless the modules are private or used
  strictly only within the context of the code declaring it.
- Flag files that aggregate those independent concerns; do not split cohesive
  models or private helpers solely by size.
- Flag dense control flow, vague names, and comments that narrate code instead
  of explaining non-obvious invariants.
- Compare sibling paths and implementations for duplicated rules or inconsistent
  validation and edge-case semantics.
- Verify failure classification, cancellation propagation, preserved exception
  causes, secret-free diagnostics, and focused negative-path tests.
- This is not an enterprise-grade project. Do not attempt to harden the software
  against every possible edge-case or conceivable situation. Workers are allowed
  (as long as you document them) to focus only on the happy path and the most
  obvious failure paths. Do not require excessive hardening.
- Flag ports not in range 65100-65199 for Docker Compose, Docker Containers and
  services in general
- Do not demand smoke tests. Limit yourself to unit and integration tests.
- Do not demand end-to-end tests. Limit yourself to unit and integration tests.
- Do not demand deployment smoke tests. Limit yourself to unit and integrations
  tests.
- Do not demand hardened, rootless, distroless, with read-only fs, any container
  image we did not make.
- Require hardended, rootless, distroless, with read-only fs, any container
  image we made.
- If an issue persists between review rounds and the proposed solutions look
  like running in circles pick a good enough solution, record any shortcoming to
  [`DEBT.md`](./DEBT.md) and move on.
  - Do not flag anything that is already documented in [`DEBT.md`](./DEBT.md).

## Session Overview

Use this template to produce a "Session Overview". You are free to omit any
parts that do not make sense for the session you are summarizing.

```markdown
Session Summary:

User Story: US-XYZ "Title"

- Worker-Review loops: 2
- Review findings: 28
- DEBT.md changes: 0
- Status requests sent: 2
- 20-min window extensions: 2
- Agents interruptions: 1
- Ran to completion: yes

This part is a brief optional summary to report interesting events such as
errors, interruptions, restarts, etc. that prevent a full completion, required
direct intervention or need human intervention.
```
